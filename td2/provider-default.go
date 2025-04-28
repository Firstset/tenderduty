package tenderduty

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/types/bech32"
	gov "github.com/cosmos/cosmos-sdk/x/gov/types"
	slashing "github.com/cosmos/cosmos-sdk/x/slashing/types"
	staking "github.com/cosmos/cosmos-sdk/x/staking/types"
)

func ConvertValopertToAccAddress(valoperAddr string) (string, error) {
	// Check if it's a valoper address
	if !strings.Contains(valoperAddr, "valoper") {
		return valoperAddr, nil // Already an account address or something else
	}

	// Decode the address
	prefix, bytes, err := bech32.DecodeAndConvert(valoperAddr)
	if err != nil {
		return "", fmt.Errorf("🌟 failed to decode valoper address: %w", err)
	}

	// Get the base prefix by removing "valoper"
	basePrefix := strings.Replace(prefix, "valoper", "", 1)

	// Re-encode with the base prefix
	accAddress, err := bech32.ConvertAndEncode(basePrefix, bytes)
	if err != nil {
		return "", fmt.Errorf("🌟 failed to encode account address: %w", err)
	}

	return accAddress, nil
}

type DefaultProvider struct {
	ChainConfig *ChainConfig
}

func (d *DefaultProvider) CheckIfValidatorVoted(ctx context.Context, proposalID uint64, accAddress string) (bool, error) {
	params := url.Values{}
	query := fmt.Sprintf("\"proposal_vote.proposal_id='%d' AND proposal_vote.voter='%s'\"", proposalID, accAddress)
	params.Add("query", query)
	params.Add("prove", "false")
	params.Add("page", "1")
	params.Add("per_page", "1")

	// Create a reusable HTTP client with timeout
	client := &http.Client{
		Timeout: 5 * time.Second, // Add reasonable timeout
	}

	// Store the last error to return if all nodes fail
	var lastErr error

	// Try each node in the list until we find a vote or exhaust all options
	for _, node := range d.ChainConfig.Nodes {
		reqURL := fmt.Sprintf("%s/tx_search?%s", node.Url, params.Encode())

		// Make the HTTP request with context
		req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
		if err != nil {
			lastErr = err
			continue // Try next node
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue // Try next node
		}

		// Use defer in a function to ensure it's called before continuing the loop
		found := false
		func() {
			defer resp.Body.Close()

			// check for existence of txs
			var result map[string]any
			if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
				lastErr = err
				return // Exit this func, continue loop
			}

			// Navigate the JSON structure to check if txs exist
			if resultObj, ok := result["result"].(map[string]any); ok {
				if txs, ok := resultObj["txs"].([]any); ok && len(txs) > 0 {
					// Set found to true so we return true outside the loop
					found = true
				}
			}
		}()

		// If we found a vote with this node, return immediately
		if found {
			return true, nil
		}

		// Otherwise, continue to next node
	}

	// If we've tried all nodes and found no votes, return false
	// If there were errors, return the last one
	if lastErr != nil {
		return false, fmt.Errorf("failed to check validator vote across all nodes: %w", lastErr)
	}

	return false, nil
}

func (d *DefaultProvider) QueryUnvotedOpenProposalIds(ctx context.Context) ([]uint64, error) {
	// get all proposals in voting period
	qProposal := gov.QueryProposalsRequest{
		// Filter for only proposals in voting period
		ProposalStatus: gov.StatusVotingPeriod,
	}
	b, err := qProposal.Marshal()
	if err == nil {
		resp, err := d.ChainConfig.client.ABCIQuery(ctx, "/cosmos.gov.v1.Query/Proposals", b)
		if resp == nil || resp.Response.Value == nil {
			return nil, fmt.Errorf("🛑 failed to query proposals for %s, error: %v", d.ChainConfig.name, err)
		} else {
			proposals := &gov.QueryProposalsResponse{}
			err = proposals.Unmarshal(resp.Response.Value)
			if err == nil {
				// Step 2: Filter out proposals the validator has already voted on
				var unvotedProposalsIds []uint64

				for _, proposal := range proposals.Proposals {
					// For each proposal, check if the validator has voted
					accAddress, err := ConvertValopertToAccAddress(d.ChainConfig.ValAddress)
					if err != nil {
						l(fmt.Sprintf("⚠️ Cannot convert valoper to account address: %v", err))
						continue
					}

					hasVoted, err := d.CheckIfValidatorVoted(ctx, proposal.ProposalId, accAddress)
					if err != nil {
						l(fmt.Sprintf("⚠️ Error checking if validator voted: %v", err))
					}

					if err == nil && !hasVoted {
						unvotedProposalsIds = append(unvotedProposalsIds, proposal.ProposalId)
					}
				}

				return unvotedProposalsIds, nil
			}
		}
	}
	return nil, err
}

func (d *DefaultProvider) QueryValidatorInfo(ctx context.Context) (pub []byte, moniker string, jailed bool, bonded bool, err error) {
	if strings.Contains(d.ChainConfig.ValAddress, "valcons") {
		_, bz, err := bech32.DecodeAndConvert(d.ChainConfig.ValAddress)
		if err != nil {
			return nil, "", false, false, errors.New("could not decode and convert your address" + d.ChainConfig.ValAddress)
		}

		hexAddress := fmt.Sprintf("%X", bz)
		return ToBytes(hexAddress), d.ChainConfig.ValAddress, false, true, nil
	}

	q := staking.QueryValidatorRequest{
		ValidatorAddr: d.ChainConfig.ValAddress,
	}
	b, err := q.Marshal()
	if err != nil {
		return
	}
	resp, err := d.ChainConfig.client.ABCIQuery(ctx, "/cosmos.staking.v1beta1.Query/Validator", b)
	if err != nil {
		return
	}
	if resp.Response.Value == nil {
		return nil, "", false, false, errors.New("could not find validator " + d.ChainConfig.ValAddress)
	}
	val := &staking.QueryValidatorResponse{}
	err = val.Unmarshal(resp.Response.Value)
	if err != nil {
		return
	}
	if val.Validator.ConsensusPubkey == nil {
		return nil, "", false, false, errors.New("got invalid consensus pubkey for " + d.ChainConfig.ValAddress)
	}

	pubBytes := make([]byte, 0)
	switch val.Validator.ConsensusPubkey.TypeUrl {
	case "/cosmos.crypto.ed25519.PubKey":
		pk := ed25519.PubKey{}
		err = pk.Unmarshal(val.Validator.ConsensusPubkey.Value)
		if err != nil {
			return
		}
		pubBytes = pk.Address().Bytes()
	case "/cosmos.crypto.secp256k1.PubKey":
		pk := secp256k1.PubKey{}
		err = pk.Unmarshal(val.Validator.ConsensusPubkey.Value)
		if err != nil {
			return
		}
		pubBytes = pk.Address().Bytes()
	}
	if len(pubBytes) == 0 {
		return nil, "", false, false, errors.New("could not get pubkey for" + d.ChainConfig.ValAddress)
	}

	return pubBytes, val.Validator.GetMoniker(), val.Validator.Jailed, val.Validator.Status == 3, nil
}

func (d *DefaultProvider) QuerySigningInfo(ctx context.Context) (*slashing.ValidatorSigningInfo, error) {
	// get current signing information (tombstoned, missed block count)
	qSigning := slashing.QuerySigningInfoRequest{ConsAddress: d.ChainConfig.valInfo.Valcons}
	b, err := qSigning.Marshal()
	if err != nil {
		return nil, fmt.Errorf("marshal signing info request: %w", err)
	}
	resp, err := d.ChainConfig.client.ABCIQuery(ctx, "/cosmos.slashing.v1beta1.Query/SigningInfo", b)
	if resp == nil || resp.Response.Value == nil {
		return nil, fmt.Errorf("query signing info: %w", err)
	}
	info := &slashing.QuerySigningInfoResponse{}
	err = info.Unmarshal(resp.Response.Value)
	if err != nil {
		return nil, fmt.Errorf("unmarshal signing info response: %w", err)
	}

	return &info.ValSigningInfo, nil
}

func (d *DefaultProvider) QuerySlashingParams(ctx context.Context) (*slashing.Params, error) {
	qParams := &slashing.QueryParamsRequest{}
	b, err := qParams.Marshal()
	if err != nil {
		return nil, fmt.Errorf("marshal slashing params: %w", err)
	}
	resp, err := d.ChainConfig.client.ABCIQuery(ctx, "/cosmos.slashing.v1beta1.Query/Params", b)
	if err != nil {
		return nil, fmt.Errorf("query slashing params: %w", err)
	}
	if resp.Response.Value == nil {
		return nil, errors.New("🛑 could not query slashing params, got empty response")
	}
	params := &slashing.QueryParamsResponse{}
	err = params.Unmarshal(resp.Response.Value)
	if err != nil {
		return nil, fmt.Errorf("unmarshal slashing params: %w", err)
	}
	return &params.Params, nil
}
