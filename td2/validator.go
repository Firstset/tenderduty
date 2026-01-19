package tenderduty

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	github_com_cosmos_cosmos_sdk_types "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/bech32"
	bank "github.com/cosmos/cosmos-sdk/x/bank/types"
	utils "github.com/firstset/tenderduty/v2/td2/utils"
)

// ValInfo holds most of the stats/info used for secondary alarms. It is refreshed roughly every minute.
type ValInfo struct {
	Moniker               string                                       `json:"moniker"`
	Bonded                bool                                         `json:"bonded"`
	Jailed                bool                                         `json:"jailed"`
	Tombstoned            bool                                         `json:"tombstoned"`
	Missed                int64                                        `json:"missed"`
	Window                int64                                        `json:"window"`
	Conspub               []byte                                       `json:"conspub"`
	Valcons               string                                       `json:"valcons"`
	DelegatedTokens       float64                                      `json:"delegated_tokens"`
	VotingPowerPercent    float64                                      `json:"voting_power_percent"`
	CommissionRate        float64                                      `json:"commission_rate"`
	ValidatorAPR          float64                                      `json:"validator_apr"`
	Projected30DRewards   float64                                      `json:"projected_30d_rewards"`
	SelfDelegationRewards *github_com_cosmos_cosmos_sdk_types.DecCoins `json:"self_delegation_rewards"`
	Commission            *github_com_cosmos_cosmos_sdk_types.DecCoins `json:"commission"`
}

// GetMinSignedPerWindow The check the minimum signed threshold of the validator.
func (cc *ChainConfig) GetMinSignedPerWindow() (err error) {
	if cc.client == nil {
		return errors.New("nil rpc client")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var provider ChainProvider
	switch cc.Provider.Name {
	case "namada":
		provider = &NamadaProvider{
			ChainConfig: cc,
		}
	default:
		provider = &DefaultProvider{
			ChainConfig: cc,
		}
	}

	slashingParams, err := provider.QuerySlashingParams(ctx)
	if err != nil {
		return
	}

	cc.minSignedPerWindow = slashingParams.MinSignedPerWindow.MustFloat64()
	return
}

func (cc *ChainConfig) fetchBankMetadataFromGitHub() (metadata *bank.Metadata, err error) {
	cacheKey := "bank_metadata_map"
	// try to find the data from cache first
	cache, ok1 := td.tenderdutyCache.Get(cacheKey)
	bankMetadataMap, ok2 := cache.(map[string]bank.Metadata)
	if !ok1 || !ok2 {
		// cache not found, fetch and cache it
		json_file := "https://raw.githubusercontent.com/Firstset/tenderduty/refs/heads/main/static/tenderduty_bank_metadata.json"
		resp, err := http.Get(json_file)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch bank metadata from GitHub: %w", err)
		}
		defer resp.Body.Close()

		// Check if status code is not 200 OK
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("failed to fetch bank metadata from GitHub: unexpected status code %d", resp.StatusCode)
		}

		decoder := json.NewDecoder(resp.Body)
		if err := decoder.Decode(&bankMetadataMap); err != nil {
			return nil, err
		}

		// cache the newly fetched data
		td.tenderdutyCache.Set(cacheKey, bankMetadataMap, 12*time.Hour)
	}

	if metadata, ok := bankMetadataMap[cc.Slug]; ok {
		return &metadata, nil
	} else {
		return nil, fmt.Errorf("no bank metadata found for %s in GitHub fallback", cc.Slug)
	}
}

// GetValInfo the first bool is used to determine if extra information about the validator should be printed.
func (cc *ChainConfig) GetValInfo(first bool) (err error) {
	if cc.client == nil {
		return errors.New("nil rpc client")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if cc.valInfo == nil {
		cc.valInfo = &ValInfo{}
	}

	var provider ChainProvider
	switch cc.Provider.Name {
	case "namada":
		provider = &NamadaProvider{
			ChainConfig: cc,
		}
	default:
		provider = &DefaultProvider{
			ChainConfig: cc,
		}
	}

	// Fetch info from /cosmos.staking.v1beta1.Query/Validator
	// it's easier to ask people to provide valoper since it's readily available on
	// explorers, so make it easy and lookup the consensus key for them.
	conspub, moniker, jailed, bonded, delegatedTokens, commissionRate, err := provider.QueryValidatorInfo(ctx)
	if err != nil {
		return
	}

	cc.valInfo.Conspub = conspub
	cc.valInfo.Moniker = moniker
	cc.valInfo.Jailed = jailed
	cc.valInfo.Bonded = bonded
	cc.valInfo.DelegatedTokens = delegatedTokens
	cc.valInfo.CommissionRate = commissionRate
	if td.PriceConversion.Enabled {
		cryptoPrice, err := td.coinMarketCapClient.GetPrice(ctx, cc.Slug)
		if err == nil {
			cc.cryptoPrice = cryptoPrice
		}
	}

	if first && cc.valInfo.Bonded {
		l(fmt.Sprintf("⚙️ found %s (%s) in validator set", cc.ValAddress, cc.valInfo.Moniker))
	} else if first && !cc.valInfo.Bonded {
		l(fmt.Sprintf("❌ %s (%s) is INACTIVE", cc.ValAddress, cc.valInfo.Moniker))
	}

	if strings.Contains(cc.ValAddress, "valcons") {
		// no need to change prefix for signing info query
		cc.valInfo.Valcons = cc.ValAddress
	} else {
		// need to know the prefix for when we serialize the slashing info query, this is too fragile.
		// for now, we perform specific chain overrides based on known values because the valoper is used
		// in so many places.
		var prefix string
		split := strings.Split(cc.ValAddress, "valoper")
		if len(split) != 2 {
			if pre, ok := altValopers.getAltPrefix(cc.ValAddress); ok {
				cc.valInfo.Valcons, err = bech32.ConvertAndEncode(pre, cc.valInfo.Conspub[:20])
				if err != nil {
					return
				}
			} else {
				err = errors.New("❓ could not determine bech32 prefix from valoper address: " + cc.ValAddress)
				return
			}
		} else {
			prefix = split[0] + "valcons"
			cc.valInfo.Valcons, err = bech32.ConvertAndEncode(prefix, cc.valInfo.Conspub[:20])
			if err != nil {
				return
			}
		}
		if first {
			l("⚙️", cc.ValAddress[:20], "... is using consensus key:", cc.valInfo.Valcons)
		}
	}

	// Query the chain's voting pool information so that we can calculate the voting power later on
	votingPool, err := provider.QueryValidatorVotingPool(ctx)
	if err == nil {
		cc.totalBondedTokens = votingPool.BondedTokens.ToDec().MustFloat64()
		cc.valInfo.VotingPowerPercent = cc.valInfo.DelegatedTokens / cc.totalBondedTokens
		// TODO:update statsChan
	} else {
		l(err)
	}

	// Query the chain's outstanding rewards
	rewards, commission, err := provider.QueryValidatorSelfDelegationRewardsAndCommission(ctx)
	if err == nil {
		// query the chain's denom metadata, only query once since this does not change
		if first && rewards != nil && len(*rewards) > 0 {
			bankMeta, err := provider.QueryDenomMetadata(ctx, (*rewards)[0].Denom)
			if err == nil {
				cc.denomMetadata = bankMeta
			} else {
				l(fmt.Errorf("cannot query bank metadata for chain %s via ABCI, err: %w, trying cosmos.directory fallback", cc.name, err))
				// Try cosmos.directory fallback first
				bankMeta = cc.getBankMetadataFromCosmosDirectory((*rewards)[0].Denom)
				if bankMeta != nil {
					cc.denomMetadata = bankMeta
					l(fmt.Sprintf("✅ loaded bank metadata for chain %s from cosmos.directory", cc.name))
				} else {
					l(fmt.Sprintf("ℹ️ cosmos.directory bank metadata not available for chain %s, trying GitHub fallback", cc.name))
					bankMeta, err = cc.fetchBankMetadataFromGitHub()
					if err == nil {
						cc.denomMetadata = bankMeta
					} else {
						l(fmt.Errorf("cannot find bank metadata for chain %s in the GitHub JSON file, err: %w", cc.name, err))
					}
				}
			}
		}

		// calculate the rewards and update valInfo.OutstandingRewards
		if cc.denomMetadata != nil && rewards != nil {
			rewardsConverted, err := utils.ConvertDecCoinToDisplayUnit(*rewards, *cc.denomMetadata)
			if err == nil {
				rewards = rewardsConverted
			} else {
				l(fmt.Errorf("cannot convert rewards to its display unit for chain %s, err: %w, the value will remain in the base unit", cc.name, err))
			}
		}

		if cc.denomMetadata != nil && commission != nil {
			commissionConverted, err := utils.ConvertDecCoinToDisplayUnit(*commission, *cc.denomMetadata)
			if err == nil {
				commission = commissionConverted
			} else {
				l(fmt.Errorf("cannot convert commission to its display unit for chain %s, err: %w, the value will remain in the base unit", cc.name, err))
			}
		}

		cc.valInfo.SelfDelegationRewards = rewards
		cc.valInfo.Commission = commission

		// TODO:update statsChan
	} else {
		l(fmt.Errorf("failed to query rewards and commission information for chain %s, err: %w", cc.name, err))
	}

	if cc.denomMetadata != nil {
		// Query the chain's base APR
		totalSupply, communityTax, inflationRate, err := provider.QueryChainInfo(ctx)
		if err == nil {
			cc.totalSupply = totalSupply
			cc.communityTax = communityTax
			if cc.InflationRateOverriding != 0 {
				// Override the base APR with the configured value
				inflationRate = cc.InflationRateOverriding
			}
			cc.inflationRate = inflationRate
			cc.baseAPR = inflationRate * (1 - communityTax) * totalSupply / cc.totalBondedTokens
			cc.valInfo.ValidatorAPR = cc.baseAPR * (1 - cc.valInfo.CommissionRate)
		} else {
			// Try cosmos.directory fallback for APR data
			cdTotalSupply, cdCommunityTax, cdAPR, ok := cc.getChainInfoFromCosmosDirectory()
			if ok && cdAPR > 0 {
				l(fmt.Sprintf("✅ using cosmos.directory APR data for chain %s (APR: %.4f)", cc.name, cdAPR))
				cc.totalSupply = cdTotalSupply
				cc.communityTax = cdCommunityTax
				// Use the pre-calculated APR from cosmos.directory as the base APR
				cc.baseAPR = cdAPR
				cc.valInfo.ValidatorAPR = cc.baseAPR * (1 - cc.valInfo.CommissionRate)
			} else {
				l(fmt.Errorf("failed to query APR-related data for chain %s via ABCI (err: %w) and cosmos.directory fallback not available", cc.name, err))
			}
		}

		// Calculate projected rewards if we have APR and price data
		if cc.baseAPR > 0 && cc.cryptoPrice != nil {
			// Calculate the validator's projected 30-day rewards in base units
			projected30DRewardsBaseUnits := cc.valInfo.DelegatedTokens * cc.valInfo.ValidatorAPR * cc.valInfo.CommissionRate * 30 / 365

			// Convert from base units to display units using exponent
			var displayExponent uint32 = 0
			if cc.denomMetadata.Display != "" {
				// Find the exponent for the display denomination
				for _, unit := range cc.denomMetadata.DenomUnits {
					if unit.Denom == cc.denomMetadata.Display {
						displayExponent = unit.Exponent
						break
					}
				}
			}

			// Convert to display units by dividing by 10^exponent
			projected30DRewardsDisplayUnits := projected30DRewardsBaseUnits
			for i := uint32(0); i < displayExponent; i++ {
				projected30DRewardsDisplayUnits = projected30DRewardsDisplayUnits / 10
			}

			// Convert to fiat currency
			cc.valInfo.Projected30DRewards = projected30DRewardsDisplayUnits * cc.cryptoPrice.Price
		}
	}

	// Query for unvoted proposals regardless of alert setting
	unvotedProposals, err := provider.QueryUnvotedOpenProposals(ctx)
	if err == nil {
		cc.unvotedOpenGovProposals = unvotedProposals
		if td.Prom {
			td.statsChan <- cc.mkUpdate(metricUnvotedProposals, float64(len(cc.unvotedOpenGovProposals)), "")
		}
	} else {
		l(err)
	}

	// Log if governance alerts are disabled (only on first run)
	if first && !boolVal(cc.Alerts.GovernanceAlerts) {
		l(fmt.Sprintf("ℹ️ Governance alerts disabled for %s (%s)", cc.ValAddress, cc.valInfo.Moniker))
	}

	signingInfo, err := provider.QuerySigningInfo(ctx)
	if err != nil {
		return
	}
	cc.valInfo.Tombstoned = signingInfo.Tombstoned
	if cc.valInfo.Tombstoned {
		l(fmt.Sprintf("❗️☠️ %s (%s) is tombstoned 🪦❗️", cc.ValAddress, cc.valInfo.Moniker))
	}
	cc.valInfo.Missed = signingInfo.MissedBlocksCounter
	if td.Prom {
		td.statsChan <- cc.mkUpdate(metricWindowMissed, float64(cc.valInfo.Missed), "")
	}

	// finally get the signed blocks window
	if cc.valInfo.Window == 0 {
		slashingParams, error := provider.QuerySlashingParams(ctx)
		if error != nil {
			return
		}
		if first && td.Prom {
			td.statsChan <- cc.mkUpdate(metricWindowSize, float64(slashingParams.SignedBlocksWindow), "")
			td.statsChan <- cc.mkUpdate(metricTotalNodes, float64(len(cc.Nodes)), "")
		}
		cc.valInfo.Window = slashingParams.SignedBlocksWindow
	}
	return
}

func ToBytes(address string) []byte {
	bz, _ := hex.DecodeString(strings.ToLower(address))
	return bz
}
