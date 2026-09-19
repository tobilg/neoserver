package datasource

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodeFeatureJSONPreservesNumbers(t *testing.T) {
	input := `{"id":9007199254740993,"properties":{"negative":-9007199254740993,"amount":123456789012345.123456789012345,"nested":[{"value":9007199254740995}],"missing":null}}`
	feature, err := DecodeFeatureJSON([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if feature["id"] != json.Number("9007199254740993") {
		t.Fatal(feature)
	}
	encoded, err := json.Marshal(feature)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"9007199254740993", "-9007199254740993", "123456789012345.123456789012345", "9007199254740995", `"missing":null`} {
		if !strings.Contains(string(encoded), token) {
			t.Errorf("lost %s in %s", token, encoded)
		}
	}
	for _, invalid := range []string{`{"id":`, `{"id":1} {}`, `{"id":1}garbage`, `[]`} {
		if _, err := DecodeFeatureJSON([]byte(invalid)); err == nil {
			t.Errorf("accepted invalid feature %q", invalid)
		}
	}
}
