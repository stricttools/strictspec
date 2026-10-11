package declarations

import "errors"

var errNotImplemented = errors.New("not implemented")

type Report struct{}

func Compare(set Set, live LiveReader, claims Claims) (Report, error) {
	return Report{}, errNotImplemented
}
func RenderImport(set Set, live Resource) ([]byte, error) { return nil, errNotImplemented }
func Apply(set Set, live LiveReader, claims Claims, w Writer, g Git, diffID string) (Report, error) {
	return Report{}, errNotImplemented
}
