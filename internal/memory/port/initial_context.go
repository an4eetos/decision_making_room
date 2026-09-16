package port

// InitialContextReader loads baseline profile text injected on every consult.
type InitialContextReader interface {
	Read() (string, error)
}
