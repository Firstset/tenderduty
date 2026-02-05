// the code about querying validator information is taken from https://github.com/ekhvalov/tenderduty/blob/main/td2/validator.go with minor modifications
package tenderduty

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	cosmos_sdk_types "github.com/cosmos/cosmos-sdk/types"
	github_com_cosmos_cosmos_sdk_types "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/bech32"
	bank "github.com/cosmos/cosmos-sdk/x/bank/types"
	gov "github.com/cosmos/cosmos-sdk/x/gov/types"
	slashing "github.com/cosmos/cosmos-sdk/x/slashing/types"
	staking "github.com/cosmos/cosmos-sdk/x/staking/types"
	namada "github.com/firstset/tenderduty/v2/td2/namada"
	"github.com/near/borsh-go"
	rpchttp "github.com/tendermint/tendermint/rpc/client/http"
)

type NamadaProvider struct {
	ChainConfig *ChainConfig
}

func getVotingPeriodProposals(httpClient *http.Client, indexers []string) ([]gov.Proposal, error) {
	// Store the last error to return if all indexer endpoints fail
	var lastErr error

	// Prepare query parameters
	params := url.Values{}
	params.Add("status", "votingPeriod")

	// Slice to store proposal IDs
	votingPeriodProposalIds := []string{}
	votingPeriodProposals := []gov.Proposal{}

	// Try each indexer in the list
	for _, indexer := range indexers {
		reqURL := fmt.Sprintf("%s/api/v1/gov/proposal?%s", indexer, params.Encode())

		// Make the HTTP request
		req, err := http.NewRequest("GET", reqURL, nil)
		if err != nil {
			lastErr = err
			continue // Try next node
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue // Try next node
		}

		func() {
			defer resp.Body.Close()

			var respJson namada.NamadaProposalResponse
			if err = json.NewDecoder(resp.Body).Decode(&respJson); err != nil {
				lastErr = err
				return
			}

			// Process each proposal
			for _, namadaProposal := range respJson.Results {
				govProposal, err := namadaProposal.ToGovProposal()
				if err != nil {
					// Log error but continue with other proposals
					l(slog.LevelWarn, fmt.Sprintf("Failed to convert proposal %s: %v", namadaProposal.ID, err))
					continue
				}
				if !slices.Contains(votingPeriodProposalIds, namadaProposal.ID) {
					votingPeriodProposals = append(votingPeriodProposals, *govProposal)
					votingPeriodProposalIds = append(votingPeriodProposalIds, namadaProposal.ID)
				}
			}
		}()

		// If we found proposals with this node, return them
		if len(votingPeriodProposals) > 0 {
			return votingPeriodProposals, nil
		}
	}

	return votingPeriodProposals, lastErr
}

func (d *NamadaProvider) QueryUnvotedOpenProposals(ctx context.Context) ([]gov.Proposal, error) {
	var unVotedProposals []gov.Proposal

	indexers, ok1 := d.ChainConfig.Provider.Configs["indexers"].([]any)
	validatorAddress, ok2 := d.ChainConfig.Provider.Configs["validator_address"].(string)
	if ok1 && ok2 {
		// Create a reusable HTTP client with timeout
		tr := &http.Transport{
			//#nosec G402 -- configurable option
			TLSClientConfig: &tls.Config{InsecureSkipVerify: td.TLSSkipVerify},
		}
		httpClient := &http.Client{
			Transport: tr,
			Timeout:   5 * time.Second, // Add reasonable timeout
		}

		urls := make([]string, len(indexers))
		for i, v := range indexers {
			if str, ok := v.(string); ok {
				urls[i] = str
			}
		}

		votingPeriodProposals, err := getVotingPeriodProposals(httpClient, urls)
		votedProposalIds := []float64{}
		if err != nil {
			return nil, err
		}

		// check voting results using different indexers
		for _, indexer := range indexers {
			reqURL := fmt.Sprintf("%s/api/v1/gov/voter/%s/votes", indexer, validatorAddress)

			// Make the HTTP request with context
			req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
			if err != nil {
				continue // Try next node
			}

			resp, err := httpClient.Do(req)
			if err != nil {
				continue // Try next node
			}

			// Use defer in a function to ensure it's called before continuing the loop
			func() {
				defer resp.Body.Close()

				var results []map[string]any
				if err = json.NewDecoder(resp.Body).Decode(&results); err != nil {
					return // Exit this func, continue loop
				}

				// check the voting results
				for _, vote := range results {
					if idFloat, ok := vote["proposalId"].(float64); ok {
						if !slices.Contains(votedProposalIds, idFloat) {
							votedProposalIds = append(votedProposalIds, idFloat)
						}
					}
				}
			}()
		}

		for _, proposal := range votingPeriodProposals {
			if !slices.Contains(votedProposalIds, float64(proposal.ProposalId)) {
				unVotedProposals = append(unVotedProposals, proposal)
			}
		}
	}

	return unVotedProposals, nil
}

func (d *NamadaProvider) QueryValidatorInfo(ctx context.Context) (pub []byte, moniker string, jailed bool, bonded bool, delegatedTokens float64, commissionRate float64, err error) {
	hexAddress := ""
	if strings.Contains(d.ChainConfig.ValAddress, "valcons") {
		_, bz, err := bech32.DecodeAndConvert(d.ChainConfig.ValAddress)
		if err != nil {
			return nil, "", false, false, 0, 0, errors.New("could not decode and convert your address " + d.ChainConfig.ValAddress)
		}
		hexAddress = fmt.Sprintf("%X", bz)
	}

	validatorAddress, ok := d.ChainConfig.Provider.Configs["validator_address"].(string)

	if ok {
		response, err := d.ChainConfig.client.ABCIQuery(ctx, fmt.Sprintf("/vp/pos/validator/state/%s", validatorAddress), nil)
		if err != nil {
			return nil, "", false, false, 0, 0, errors.New("failed to query Namada validator's state " + validatorAddress)
		}

		state := namada.ValidatorStateInfo{}
		err = borsh.Deserialize(&state, response.Response.Value)
		if err != nil {
			return nil, "", false, false, 0, 0, fmt.Errorf("unmarshal validator state: %w", err)
		}
		info := ValInfo{}
		info.Bonded = state.State != nil && *state.State == namada.ValidatorStateConsensus
		info.Jailed = state.State != nil && *state.State == namada.ValidatorStateJailed

		response, err = d.ChainConfig.client.ABCIQuery(ctx, fmt.Sprintf("/vp/pos/validator/metadata/%s", validatorAddress), nil)
		if err != nil {
			return nil, "", false, false, 0, 0, fmt.Errorf("query validator metadata: %w", err)
		}
		metadata := namada.ValidatorMetaData{}
		err = borsh.Deserialize(&metadata, response.Response.Value)
		if err != nil {
			return nil, "", false, false, 0, 0, fmt.Errorf("unmarshal validator metadata: %w", err)
		}
		if metadata.Metadata != nil && metadata.Metadata.Name != nil {
			info.Moniker = *metadata.Metadata.Name
		}

		response, err = d.ChainConfig.client.ABCIQuery(ctx, fmt.Sprintf("/vp/pos/validator/stake/%s", validatorAddress), nil)
		if err != nil {
			return nil, "", false, false, 0, 0, fmt.Errorf("query validator stake: %w", err)
		}
		var stake *namada.Dec
		err = borsh.Deserialize(&stake, response.Response.Value)
		if err != nil {
			return nil, "", false, false, 0, 0, fmt.Errorf("unmarshal validator stake: %w", err)
		}
		if stake != nil {
			delegatedTokensFloat, err := strconv.ParseFloat(stake.Raw.String(), 64)
			if err == nil {
				// not sure about the rationale behind yet but the value is uint and it needs to be divivded by 1e6 to get the correct precision
				info.DelegatedTokens = delegatedTokensFloat / 1000000
			}
		}

		response, err = d.ChainConfig.client.ABCIQuery(ctx, fmt.Sprintf("/vp/pos/validator/commission/%s", validatorAddress), nil)
		if err != nil {
			return nil, "", false, false, 0, 0, fmt.Errorf("query validator commission rate: %w", err)
		}
		commission := namada.ValidatorCommissionPair{}
		err = borsh.Deserialize(&commission, response.Response.Value)
		if err != nil {
			return nil, "", false, false, 0, 0, fmt.Errorf("unmarshal validator commission pair: %w", err)
		}
		if commission.CommissionRate != nil {
			commissionRateFloat, err := strconv.ParseFloat((*commission.CommissionRate).String(), 64)
			if err == nil {
				info.CommissionRate = commissionRateFloat
			}
		}
		return ToBytes(hexAddress), info.Moniker, info.Jailed, info.Bonded, info.DelegatedTokens, info.CommissionRate, nil
	}

	return ToBytes(hexAddress), d.ChainConfig.ValAddress, false, true, 0, 0, nil
}

func getLivenessInfo(ctx context.Context, client *rpchttp.HTTP) (*namada.LivenessInfo, error) {
	resp, err := client.ABCIQuery(ctx, "/vp/pos/validator/liveness_info", nil)
	if err != nil {
		return nil, fmt.Errorf("query validator liveness_info: %w", err)
	}

	livenessInfo := namada.LivenessInfo{}
	err = borsh.Deserialize(&livenessInfo, resp.Response.Value)
	if err != nil {
		return nil, fmt.Errorf("unmarshal liveness info: %w", err)
	}

	return &livenessInfo, nil
}

func (d *NamadaProvider) QuerySigningInfo(ctx context.Context) (*slashing.ValidatorSigningInfo, error) {
	livenessInfo, err := getLivenessInfo(ctx, d.ChainConfig.client)
	if err != nil {
		return nil, err
	}

	signingInfo := slashing.ValidatorSigningInfo{}
	hexAddress := strings.ToUpper(hex.EncodeToString(d.ChainConfig.valInfo.Conspub))
	for _, v := range livenessInfo.Validators {
		if v.CometAddress == hexAddress {
			// Safe conversion with overflow check
			if v.MissedVotes > 9223372036854775807 { // max int64
				return nil, fmt.Errorf("MissedVotes too large for int64: %d", v.MissedVotes)
			}
			signingInfo.MissedBlocksCounter = int64(v.MissedVotes)
		}
	}

	return &signingInfo, nil
}

func (d *NamadaProvider) QuerySlashingParams(ctx context.Context) (*slashing.Params, error) {
	livenessInfo, err := getLivenessInfo(ctx, d.ChainConfig.client)
	if err != nil {
		return nil, err
	}

	// Safe conversion with overflow check
	if livenessInfo.LivenessWindowLen > 9223372036854775807 { // max int64
		return nil, fmt.Errorf("LivenessWindowLen too large for int64: %d", livenessInfo.LivenessWindowLen)
	}

	return &slashing.Params{SignedBlocksWindow: int64(livenessInfo.LivenessWindowLen), MinSignedPerWindow: cosmos_sdk_types.MustNewDecFromStr(livenessInfo.LivenessThreshold.String())}, nil
}

func (d *NamadaProvider) QueryStakingParams(ctx context.Context) (*staking.Params, error) {
	return nil, errors.New("QueryStakingParams with ABCIQuery not implemented for Namada")
}

func (d *NamadaProvider) QueryDenomMetadata(ctx context.Context, denom string) (medatada *bank.Metadata, err error) {
	return nil, errors.New("QueryDenomMetadata with ABCIQuery not implemented for Namada")
}

func (d *NamadaProvider) QueryValidatorSelfDelegationRewardsAndCommission(ctx context.Context) (rewards *github_com_cosmos_cosmos_sdk_types.DecCoins, commission *github_com_cosmos_cosmos_sdk_types.DecCoins, err error) {
	// Store the last error to return if all indexer endpoints fail
	var lastErr error
	// In Namada we don't query self-delegation rewards, this field will be kept as 0
	resultRewards := github_com_cosmos_cosmos_sdk_types.DecCoins{
		github_com_cosmos_cosmos_sdk_types.NewDecCoin("unam", github_com_cosmos_cosmos_sdk_types.ZeroInt()),
	}
	resultCommission := github_com_cosmos_cosmos_sdk_types.DecCoins{
		github_com_cosmos_cosmos_sdk_types.NewDecCoin("unam", github_com_cosmos_cosmos_sdk_types.ZeroInt()),
	}

	indexers, ok1 := d.ChainConfig.Provider.Configs["indexers"].([]any)
	validatorAddress, ok2 := d.ChainConfig.Provider.Configs["validator_address"].(string)
	if ok1 && ok2 {
		// Create a reusable HTTP client with timeout
		tr := &http.Transport{
			//#nosec G402 -- configurable option
			TLSClientConfig: &tls.Config{InsecureSkipVerify: td.TLSSkipVerify},
		}
		httpClient := &http.Client{
			Transport: tr,
			Timeout:   5 * time.Second, // Add reasonable timeout
		}
		// Try each indexer in the list
		for _, indexer := range indexers {
			reqURL := fmt.Sprintf("%s/api/v1/pos/reward/%s", indexer, validatorAddress)

			// Make the HTTP request
			req, err := http.NewRequest("GET", reqURL, nil)
			if err != nil {
				lastErr = err
				continue // Try next node
			}

			resp, err := httpClient.Do(req)
			if err != nil {
				lastErr = err
				continue // Try next node
			}

			func() {
				defer resp.Body.Close()

				var respJson []namada.NamadaValidatorRewardsResponse
				if err = json.NewDecoder(resp.Body).Decode(&respJson); err != nil {
					lastErr = err
					return
				}

				if len(respJson) > 0 {
					value, ok := github_com_cosmos_cosmos_sdk_types.NewIntFromString(respJson[0].MinDenomAmount)
					if ok {
						resultCommission[0].Amount = value.ToDec()
					}
				}
			}()

			if resultCommission[0].Amount.IsPositive() {
				// means the query was successful
				return &resultRewards, &resultCommission, nil
			}
		}
	}
	return &resultRewards, &resultCommission, lastErr
}

func (d *NamadaProvider) QueryValidatorVotingPool(ctx context.Context) (votingPool *staking.Pool, err error) {
	// Store the last error to return if all indexer endpoints fail
	var lastErr error
	var result *staking.Pool
	indexers, ok := d.ChainConfig.Provider.Configs["indexers"].([]any)

	if ok {
		// Create a reusable HTTP client with timeout
		tr := &http.Transport{
			//#nosec G402 -- configurable option
			TLSClientConfig: &tls.Config{InsecureSkipVerify: td.TLSSkipVerify},
		}
		httpClient := &http.Client{
			Transport: tr,
			Timeout:   5 * time.Second, // Add reasonable timeout
		}
		// Try each indexer in the list
		for _, indexer := range indexers {
			reqURL := fmt.Sprintf("%s/api/v1/pos/voting-power", indexer)

			// Make the HTTP request
			req, err := http.NewRequest("GET", reqURL, nil)
			if err != nil {
				lastErr = err
				continue // Try next node
			}

			resp, err := httpClient.Do(req)
			if err != nil {
				lastErr = err
				continue // Try next node
			}

			func() {
				defer resp.Body.Close()

				var respJson namada.NamadaVotingPowerResponse
				if err = json.NewDecoder(resp.Body).Decode(&respJson); err != nil {
					lastErr = err
					return
				}

				bondedTokens, ok := github_com_cosmos_cosmos_sdk_types.NewIntFromString(respJson.TotalVotingPower)
				if ok {
					result = &staking.Pool{
						NotBondedTokens: github_com_cosmos_cosmos_sdk_types.ZeroInt(), // we ommit this field in Namada
						BondedTokens:    bondedTokens,
					}
				}
			}()

			if result != nil {
				return result, nil
			}
		}
	}
	return nil, lastErr
}

func (d *NamadaProvider) QueryChainInfo(ctx context.Context) (totalSupply float64, communityTax float64, inflationRate float64, err error) {
	// TODO: leave it here for now, Namada has a quite different way of calculating the inflation rate
	// see more details here https://specs.namada.net/modules/proof-of-stake/inflation-system#proof-of-stake-rewards
	return 0, 0, 0, errors.New("CalculateAPR not implemented for Namada")
}
