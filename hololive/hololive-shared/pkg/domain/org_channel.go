package domain

type Channel struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	EnglishName *string `json:"english_name,omitempty"`
	Photo       *string `json:"photo,omitempty"`
	Twitter     *string `json:"twitter,omitempty"`
	Org         *string `json:"org,omitempty"`
	Suborg      *string `json:"suborg,omitempty"`
	Group       *string `json:"group,omitempty"`
}
