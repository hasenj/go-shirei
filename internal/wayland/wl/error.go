package wl

// combinedError is a tuple of an External and an Internal error
type combinedError [2]error

func (c combinedError) Error() string {
	return c[0].Error() + ": " + c[1].Error()
}
func (c combinedError) Unwrap() error {
	return c[1]
}
func (c combinedError) External() error {
	return c[0]
}
