package discord

// Activity types Discord understands.
const (
	TypePlaying   = 0
	TypeListening = 2
)

// Values for Activity.StatusDisplayType.
const (
	StatusDisplayName    = 0
	StatusDisplayState   = 1
	StatusDisplayDetails = 2
)

// Activity is the Rich Presence payload. Field names follow Discord's API.
type Activity struct {
	Type              int         `json:"type"`
	StatusDisplayType *int        `json:"status_display_type,omitempty"`
	Details           string      `json:"details,omitempty"`
	State             string      `json:"state,omitempty"`
	Timestamps        *Timestamps `json:"timestamps,omitempty"`
	Assets            *Assets     `json:"assets,omitempty"`
	Buttons           []Button    `json:"buttons,omitempty"`
	Instance          bool        `json:"instance"`
}

// Timestamps are Unix milliseconds. With both set, Discord draws a progress bar.
type Timestamps struct {
	Start int64 `json:"start,omitempty"`
	End   int64 `json:"end,omitempty"`
}

// Assets are image URLs (https) or asset keys uploaded to the Discord application.
type Assets struct {
	LargeImage string `json:"large_image,omitempty"`
	LargeText  string `json:"large_text,omitempty"`
	SmallImage string `json:"small_image,omitempty"`
	SmallText  string `json:"small_text,omitempty"`
}

type Button struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}
