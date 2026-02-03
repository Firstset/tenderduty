package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	github_com_cosmos_cosmos_sdk_types "github.com/cosmos/cosmos-sdk/types"
	bank "github.com/cosmos/cosmos-sdk/x/bank/types"
)

const (
	defaultCoinmarketcapApiEndpoint = "https://pro-api.coinmarketcap.com"
	defaultRequestTimeout           = 10 * time.Second
	cacheKey                        = "crypto_price"
)

// CryptoPrice represents price data for a cryptocurrency
type CryptoPrice struct {
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Symbol      string    `json:"symbol"`
	Currency    string    `json:"currency"`
	Price       float64   `json:"price"`
	LastUpdated time.Time `json:"last_updated"`
}

// CMCResponse represents the structure of the CoinMarketCap API response
type CMCResponse struct {
	Status struct {
		Timestamp    string `json:"timestamp"`
		ErrorCode    int    `json:"error_code"`
		ErrorMessage string `json:"error_message"`
	} `json:"status"`
	Data map[string]struct {
		ID     int    `json:"id"`
		Name   string `json:"name"`
		Symbol string `json:"symbol"`
		Slug   string `json:"slug"`
		Quote  map[string]struct {
			Price       float64 `json:"price"`
			LastUpdated string  `json:"last_updated"`
		} `json:"quote"`
	} `json:"data"`
}

// CoinMarketCapClient handles API requests to CoinMarketCap
type CoinMarketCapClient struct {
	apiKey          string
	currency        string
	cacheExpiration int
	slugs           []string
	apiEndpoint     string
	httpClient      *http.Client
	cacheClient     *TenderdutyCache
}

// NewCoinMarketCapClient creates a new client with the provided API key
func NewCoinMarketCapClient(apiKey string, currency string, cacheClient *TenderdutyCache, cacheExpiration int, slugs []string) *CoinMarketCapClient {
	client := &CoinMarketCapClient{
		apiKey:          apiKey,
		currency:        currency,
		cacheExpiration: cacheExpiration,
		cacheClient:     cacheClient,
		slugs:           slugs,
		apiEndpoint:     defaultCoinmarketcapApiEndpoint,
		httpClient: &http.Client{
			Timeout: defaultRequestTimeout,
		},
	}

	return client
}

// WithEndpoint allows customizing the API endpoint URL
func WithEndpoint(endpoint string) func(*CoinMarketCapClient) {
	return func(c *CoinMarketCapClient) {
		c.apiEndpoint = endpoint
	}
}

// WithTimeout allows customizing the HTTP client timeout
func WithTimeout(timeout time.Duration) func(*CoinMarketCapClient) {
	return func(c *CoinMarketCapClient) {
		c.httpClient.Timeout = timeout
	}
}

// GetPrices fetches cryptocurrency prices, using cache when available
func (c *CoinMarketCapClient) GetPrices(ctx context.Context) (map[string]CryptoPrice, error) {
	// try to find the data from cache first
	cache, ok1 := c.cacheClient.Get(cacheKey)
	prices, ok2 := cache.(map[string]CryptoPrice)

	if !ok1 || !ok2 {
		// cache nout found, fetch and cache it
		var err error
		prices, err = c.fetchPricesFromAPI(ctx, c.slugs, c.currency)
		if err != nil {
			return nil, err
		}
		// Update cache
		c.cacheClient.Set(cacheKey, prices, time.Duration(c.cacheExpiration)*time.Hour)
	}

	return prices, nil
}

// GetPrice fetches the price for a specific cryptocurrency slug, using cache when available
func (c *CoinMarketCapClient) GetPrice(ctx context.Context, slug string) (*CryptoPrice, error) {
	prices, err := c.GetPrices(ctx)
	if err != nil {
		return nil, err
	}

	if prices != nil {
		// Check if the slug exists in the freshly fetched data
		if price, exists := prices[slug]; exists {
			return &price, nil
		}
	}

	// Slug not found even after refreshing the data
	return nil, fmt.Errorf("slug '%s' not found", slug)
}

// fetchPricesFromAPI makes the actual API call to CoinMarketCap
func (c *CoinMarketCapClient) fetchPricesFromAPI(ctx context.Context, slugs []string, currency string) (map[string]CryptoPrice, error) {
	result := make(map[string]CryptoPrice)
	url := c.apiEndpoint + "/v2/cryptocurrency/quotes/latest"

	// Process each slug individually as some of the slugs may not be valid
	for _, slug := range slugs {
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			continue // Skip this slug and try the next one
		}

		// Add required headers
		req.Header.Add("X-CMC_PRO_API_KEY", c.apiKey)
		req.Header.Add("Accept", "application/json")

		// Add query parameters for this individual slug
		q := req.URL.Query()
		q.Add("slug", slug)
		q.Add("convert", currency)
		req.URL.RawQuery = q.Encode()

		resp, err := c.httpClient.Do(req)
		if err != nil {
			// Log the error and continue with next slug
			fmt.Printf("Error fetching data for slug %s: %v\n", slug, err)
			continue
		}

		// Always close the response body
		defer func(slug string, body io.ReadCloser) {
			if err := body.Close(); err != nil {
				fmt.Printf("Error closing response body for slug %s: %v\n", slug, err)
			}
		}(slug, resp.Body)

		// If status is not OK, skip this slug
		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			fmt.Printf("API error for slug %s (status %d): %s\n", slug, resp.StatusCode, string(bodyBytes))
			continue
		}

		// Parse the response
		var cmcResp CMCResponse
		if err := json.NewDecoder(resp.Body).Decode(&cmcResp); err != nil {
			fmt.Printf("Failed to parse API response for slug %s: %v\n", slug, err)
			continue
		}

		// Check for API error
		if cmcResp.Status.ErrorCode != 0 {
			fmt.Printf("API returned error for slug %s: %s\n", slug, cmcResp.Status.ErrorMessage)
			continue
		}

		// Extract the price data for this slug
		for _, cryptoData := range cmcResp.Data {
			quoteData, ok := cryptoData.Quote[currency]
			if !ok {
				continue // Skip if the requested currency quote isn't available
			}

			lastUpdated, err := time.Parse("2006-01-02T15:04:05.000Z", quoteData.LastUpdated)
			if err != nil {
				// If time parsing fails, use current time as fallback
				lastUpdated = time.Now()
			}

			result[cryptoData.Slug] = CryptoPrice{
				Name:        cryptoData.Name,
				Symbol:      cryptoData.Symbol,
				Slug:        cryptoData.Slug,
				Currency:    currency,
				Price:       quoteData.Price,
				LastUpdated: lastUpdated,
			}
		}
	}

	// Return whatever valid data we were able to gather
	return result, nil
}

// joinStrings joins strings with a separator
func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}

	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}

	return result
}

// ConvertDecCoinToDisplayUnit converts a DecCoin array to the display unit based on DenomMetadata.
func ConvertDecCoinToDisplayUnit(coins []github_com_cosmos_cosmos_sdk_types.DecCoin, metadata bank.Metadata) (*github_com_cosmos_cosmos_sdk_types.DecCoins, error) {
	var convertedCoins github_com_cosmos_cosmos_sdk_types.DecCoins

	// Find the display denomination unit
	var displayDenom string
	var displayExponent uint32

	// If no display is set, default to base
	if metadata.Display == "" {
		displayDenom = metadata.Base
	} else {
		displayDenom = metadata.Display
	}
	if err := github_com_cosmos_cosmos_sdk_types.ValidateDenom(displayDenom); err != nil {
		return nil, fmt.Errorf("invalid display denom %q for base %q: %w", displayDenom, metadata.Base, err)
	}

	// Find the exponent for the display denom
	foundDisplayDenom := false
	for _, unit := range metadata.DenomUnits {
		if unit.Denom == displayDenom {
			displayExponent = unit.Exponent
			foundDisplayDenom = true
			break
		}
	}

	if !foundDisplayDenom {
		return nil, fmt.Errorf("display unit '%s' not found in denomination units for: %s", displayDenom, metadata.Base)
	}

	for _, coin := range coins {
		// If the coin is already in the display denomination, just add it
		if coin.Denom == displayDenom {
			convertedCoins = append(convertedCoins, coin)
			continue
		}

		// Find current coin's exponent to properly calculate conversion
		var currentExponent uint32 = 0
		foundCurrentDenom := false
		for _, unit := range metadata.DenomUnits {
			if unit.Denom == coin.Denom {
				currentExponent = unit.Exponent
				foundCurrentDenom = true
				break
			}
		}

		if !foundCurrentDenom {
			// source denomination not found in denomination units
			// the coin is most likely bridged to the chain so we keep it as is
			convertedCoins = append(convertedCoins, coin)
			continue
		}

		// Calculate the conversion factor based on exponent difference
		var convertedAmount github_com_cosmos_cosmos_sdk_types.Dec

		if currentExponent < displayExponent {
			// Converting from smaller to larger unit (e.g., uatom -> atom)
			// We need to divide by 10^(display_exp - current_exp)
			exponentDiff := displayExponent - currentExponent

			// Create a decimal with the proper power of 10
			divisor := github_com_cosmos_cosmos_sdk_types.OneDec()
			for i := uint32(0); i < exponentDiff; i++ {
				divisor = divisor.MulInt64(10)
			}

			convertedAmount = coin.Amount.Quo(divisor)
		} else {
			// Converting from larger to smaller unit (e.g., atom -> uatom)
			// We need to multiply by 10^(current_exp - display_exp)
			exponentDiff := currentExponent - displayExponent

			// Create a decimal with the proper power of 10
			multiplier := github_com_cosmos_cosmos_sdk_types.OneDec()
			for i := uint32(0); i < exponentDiff; i++ {
				multiplier = multiplier.MulInt64(10)
			}

			convertedAmount = coin.Amount.Mul(multiplier)
		}

		convertedCoins = append(convertedCoins, github_com_cosmos_cosmos_sdk_types.NewDecCoinFromDec(displayDenom, convertedAmount))
	}

	return &convertedCoins, nil
}

// ConvertFloatInBaseUnitToDisplayUnit converts a float64 to the display unit based on DenomMetadata.
// return converted value, unit, and error if any
func ConvertFloatInBaseUnitToDisplayUnit(value float64, metadata bank.Metadata) (float64, string, error) {
	// Find the display denomination unit
	var displayDenom string
	var displayExponent uint32
	var convertedValue float64

	// If no display is set, default to base
	if metadata.Display == "" {
		displayDenom = metadata.Base
		if err := github_com_cosmos_cosmos_sdk_types.ValidateDenom(displayDenom); err != nil {
			return 0, "", fmt.Errorf("invalid display denom %q for base %q: %w", displayDenom, metadata.Base, err)
		}
		// If display is base, no conversion needed
		return value, displayDenom, nil
	} else {
		displayDenom = metadata.Display
	}
	if err := github_com_cosmos_cosmos_sdk_types.ValidateDenom(displayDenom); err != nil {
		return 0, "", fmt.Errorf("invalid display denom %q for base %q: %w", displayDenom, metadata.Base, err)
	}

	// Find the exponent for the display denom
	foundDisplayDenom := false
	for _, unit := range metadata.DenomUnits {
		if unit.Denom == displayDenom {
			displayExponent = unit.Exponent
			foundDisplayDenom = true
			break
		}
	}

	if !foundDisplayDenom {
		return 0, "", fmt.Errorf("display unit '%s' not found in denomination units for: %s", displayDenom, metadata.Base)
	}

	// Convert from base unit to display unit
	// Since we're going from base (smaller unit) to display (larger unit),
	// we need to divide by 10^(display_exp - base_exp)
	divisor := 1.0
	for i := uint32(0); i < displayExponent; i++ {
		divisor *= 10.0
	}
	convertedValue = value / divisor

	return convertedValue, displayDenom, nil
}
