package api

type API interface {
	// Get returns an image URL
	Get() (string, error)
}
