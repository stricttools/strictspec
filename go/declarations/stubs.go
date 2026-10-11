package declarations

import "errors"

var errNotImplemented = errors.New("not implemented")

func RenderImport(set Set, live Resource) ([]byte, error) { return nil, errNotImplemented }
