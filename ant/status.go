package ant

type Status int

const (
	NotStarted Status = iota
	Started
	Downloading
	Completed
	Aborted
	Destroyed
)

func IsStarted(s Status) bool {
	return s == Started || s == Downloading
}

func IsCompleted(s Status) bool {
	return s == Completed || s == Aborted || s == Destroyed
}
