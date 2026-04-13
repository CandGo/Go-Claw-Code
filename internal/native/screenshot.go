package native

import (
	"time"
)

// ScreenshotResult holds captured screen data.
type ScreenshotResult struct {
	Width     int
	Height    int
	Data      []byte // PNG-encoded bytes
	Monitor   int    // monitor index (0 = primary, -1 = all)
	Timestamp time.Time
}
