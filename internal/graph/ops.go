package graph

// Rename is one planned/executed file move, From -> To. Renames keep file
// bytes; only paths change.
type Rename struct {
	From string
	To   string
}

// Rewrite is one executed reference rewrite inside a JSON file: the string
// at (File, Pointer) changed From -> To. File is the pack path the JSON was
// read from BEFORE any rename of that JSON file itself, so logs stay stable
// against input coordinates.
type Rewrite struct {
	File    string
	Pointer string
	From    string
	To      string
}
