package update

// UpdateAsset is one downloadable release artifact. Candidates are returned
// ordered by preference, so downloaders can simply try them in order.
type UpdateAsset struct {
	Name   string `json:"name"`   // filename, e.g. uniterm-linux-amd64-v1.9.2.tar.gz
	URL    string `json:"url"`    // direct download URL
	SHA256 string `json:"sha256"` // expected sha256 hex from the signed checksums.txt
	Source string `json:"source"` // always "github" (r1ckcs/uniterm)
}

// UpdateInfo is the result returned to the frontend.
type UpdateInfo struct {
	HasUpdate  bool          `json:"hasUpdate"`
	Current    string        `json:"current"`
	Latest     string        `json:"latest"`
	ReleaseURL string        `json:"releaseUrl"`
	Changelog  string        `json:"changelog"`
	Assets     []UpdateAsset `json:"assets"`
}

// Progress is a single update-progress event emitted to the frontend.
type Progress struct {
	Phase    string  `json:"phase"` // "downloading" | "verifying" | "applying" | "error"
	Received int64   `json:"received"`
	Total    int64   `json:"total"` // -1 when unknown
	Percent  float64 `json:"percent"`
	Message  string  `json:"message,omitempty"`
}
