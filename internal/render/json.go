package render

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/JaydenCJ/flakesift/internal/version"
)

// envelope is the stable machine-readable wrapper. schema_version only
// changes on breaking shape changes; consumers should pin on it.
type envelope struct {
	Tool          string `json:"tool"`
	Version       string `json:"version"`
	SchemaVersion int    `json:"schema_version"`
	Kind          string `json:"kind"`
	Data          any    `json:"data"`
}

// JSON writes payload wrapped in the flakesift envelope, indented, with a
// trailing newline. No timestamps are embedded: identical input must
// produce byte-identical output.
func JSON(w io.Writer, kind string, payload any) error {
	env := envelope{
		Tool:          "flakesift",
		Version:       version.Version,
		SchemaVersion: 1,
		Kind:          kind,
		Data:          payload,
	}
	out, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", out)
	return err
}
