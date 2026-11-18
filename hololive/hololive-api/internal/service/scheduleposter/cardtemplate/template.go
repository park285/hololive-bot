package cardtemplate

type ID string

const (
	Classic ID = "classic"
	Orbit   ID = "orbit"
)

func (id ID) Valid() bool {
	return id == Classic || id == Orbit
}
