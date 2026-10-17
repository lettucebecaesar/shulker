package pack

// Severity of a finding. Errors make `shulker check` exit non-zero and block
// squashing; warnings never block anything, they only inform.
type Severity int

const (
	SeverityInfo Severity = iota
	SeverityWarning
	SeverityError
)

func (s Severity) String() string {
	switch s {
	case SeverityError:
		return "error"
	case SeverityWarning:
		return "warning"
	default:
		return "info"
	}
}

// Finding is a single observation about a pack: where, what, how bad.
type Finding struct {
	Severity Severity
	Path     string
	Msg      string
}

func (f Finding) Error() string {
	return f.String()
}

func (f Finding) String() string {
	if f.Path == "" {
		return f.Severity.String() + ": " + f.Msg
	}
	return f.Severity.String() + " [" + f.Path + "]: " + f.Msg
}
