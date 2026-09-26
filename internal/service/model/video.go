package model

type Platform string

const (
	YouTube   Platform = "youtube"
	Instagram Platform = "instagram"
	TikTok    Platform = "tiktok"
)

// File has a content SHA-256 identifier and an extension-preserving file name.
type File struct {
	Hash string `json:"hash"`
	Name string `json:"name"`
}
