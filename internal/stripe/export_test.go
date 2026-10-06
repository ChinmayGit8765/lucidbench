package stripe

import "github.com/ChinmayGit8765/lucidbench/internal/config"

func configFor(apiURL string) config.StripeConfig {
	return config.StripeConfig{Key: config.DefaultStripeKey, APIURL: apiURL}
}
