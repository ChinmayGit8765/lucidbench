package themes

// Presets are the built-in themes. Midnight and Daylight are the stock dark
// and light tokens in index.css, so they set nothing.
var Presets = []Theme{
	{
		ID: "midnight", Name: "Midnight", Version: Version, Base: "dark",
		Description: "The default: deep blue-grey with lucid azure light.",
		Tokens:      map[string]string{},
	},
	{
		ID: "daylight", Name: "Daylight", Version: Version, Base: "light",
		Description: "Bright and clean, with the same azure accent.",
		Tokens:      map[string]string{},
	},
	{
		ID: "graphite", Name: "Graphite", Version: Version, Base: "dark",
		Description: "Neutral greys with a silver accent. No colour unless it means something.",
		Tokens: map[string]string{
			"--background":         "oklch(0.165 0 0)",
			"--sidebar":            "oklch(0.18 0 0)",
			"--card":               "oklch(0.2 0 0)",
			"--card-foreground":    "oklch(0.95 0 0)",
			"--elevated":           "oklch(0.225 0 0)",
			"--foreground":         "oklch(0.95 0 0)",
			"--muted":              "oklch(0.24 0 0)",
			"--muted-foreground":   "oklch(0.72 0 0)",
			"--subtle-foreground":  "oklch(0.6 0 0)",
			"--secondary":          "oklch(0.255 0 0)",
			"--accent":             "oklch(0.265 0 0)",
			"--border":             "oklch(1 0 0 / 9%)",
			"--border-strong":      "oklch(1 0 0 / 14%)",
			"--primary":            "oklch(0.93 0 0)",
			"--primary-foreground": "oklch(0.18 0 0)",
			"--ring":               "oklch(0.82 0.02 250)",
			"--brand":              "oklch(0.84 0.02 250)",
			"--brand-soft":         "oklch(0.84 0.02 250 / 11%)",
			"--brand-fg":           "oklch(0.9 0.015 250)",
			"--brand-2":            "oklch(0.68 0 0)",
			"--glow-1":             "oklch(0.85 0 0 / 5%)",
			"--glow-2":             "oklch(0.6 0 0 / 4%)",
			"--info":               "oklch(0.8 0.04 250)",
			"--info-soft":          "oklch(0.8 0.04 250 / 12%)",
			"--info-fg":            "oklch(0.86 0.03 250)",
		},
	},
	{
		ID: "aurora", Name: "Aurora", Version: Version, Base: "dark",
		Description: "Deep night teal with a green-to-violet glow.",
		Tokens: map[string]string{
			"--background":         "oklch(0.16 0.02 230)",
			"--sidebar":            "oklch(0.175 0.022 230)",
			"--card":               "oklch(0.195 0.024 228)",
			"--card-foreground":    "oklch(0.96 0.01 200)",
			"--elevated":           "oklch(0.22 0.026 228)",
			"--foreground":         "oklch(0.96 0.01 200)",
			"--muted":              "oklch(0.235 0.025 228)",
			"--muted-foreground":   "oklch(0.75 0.03 210)",
			"--subtle-foreground":  "oklch(0.64 0.03 215)",
			"--secondary":          "oklch(0.25 0.026 228)",
			"--accent":             "oklch(0.265 0.03 225)",
			"--border":             "oklch(0.85 0.08 190 / 10%)",
			"--border-strong":      "oklch(0.85 0.08 190 / 16%)",
			"--input":              "oklch(0.85 0.08 190 / 16%)",
			"--primary":            "oklch(0.8 0.14 175)",
			"--primary-foreground": "oklch(0.18 0.03 200)",
			"--ring":               "oklch(0.8 0.14 175)",
			"--brand":              "oklch(0.8 0.14 175)",
			"--brand-soft":         "oklch(0.8 0.14 175 / 14%)",
			"--brand-fg":           "oklch(0.86 0.12 175)",
			"--brand-2":            "oklch(0.7 0.17 300)",
			"--glow-1":             "oklch(0.8 0.14 175 / 12%)",
			"--glow-2":             "oklch(0.7 0.17 300 / 10%)",
			"--info":               "oklch(0.78 0.12 210)",
			"--info-soft":          "oklch(0.78 0.12 210 / 14%)",
			"--info-fg":            "oklch(0.86 0.09 210)",
		},
	},
	{
		ID: "paper", Name: "Paper", Version: Version, Base: "light",
		Description: "Warm off-white and ink, with a terracotta accent and crisper corners.",
		Tokens: map[string]string{
			"--radius":             "0.375rem",
			"--background":         "oklch(0.975 0.008 85)",
			"--sidebar":            "oklch(0.955 0.012 85)",
			"--card":               "oklch(0.99 0.005 85)",
			"--card-foreground":    "oklch(0.24 0.015 60)",
			"--elevated":           "oklch(0.995 0.004 85)",
			"--foreground":         "oklch(0.24 0.015 60)",
			"--muted":              "oklch(0.945 0.012 85)",
			"--muted-foreground":   "oklch(0.46 0.02 60)",
			"--subtle-foreground":  "oklch(0.56 0.02 65)",
			"--secondary":          "oklch(0.945 0.012 85)",
			"--accent":             "oklch(0.925 0.016 80)",
			"--border":             "oklch(0.89 0.015 80)",
			"--border-strong":      "oklch(0.83 0.018 75)",
			"--input":              "oklch(0.85 0.018 75)",
			"--primary":            "oklch(0.3 0.02 60)",
			"--primary-foreground": "oklch(0.98 0.005 85)",
			"--ring":               "oklch(0.52 0.13 38)",
			"--brand":              "oklch(0.52 0.13 38)",
			"--brand-soft":         "oklch(0.52 0.13 38 / 9%)",
			"--brand-fg":           "oklch(0.46 0.12 38)",
			"--brand-2":            "oklch(0.5 0.08 150)",
			"--glow-1":             "oklch(0.75 0.08 70 / 7%)",
			"--glow-2":             "oklch(0.6 0.1 38 / 4%)",
			"--shadow-card":        "0 1px 0 oklch(0.3 0.03 60 / 6%), 0 1px 2px oklch(0.3 0.03 60 / 4%)",
		},
	},
}

// IsPreset reports whether id names a built-in theme (or "system", which
// follows the OS between Midnight and Daylight).
func IsPreset(id string) bool {
	if id == "system" {
		return true
	}
	for _, p := range Presets {
		if p.ID == id {
			return true
		}
	}
	return false
}
