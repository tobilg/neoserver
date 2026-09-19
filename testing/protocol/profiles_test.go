package protocol

import (
	"testing"

	"github.com/tobilg/neoserver/internal/conformance"
)

func TestEveryAdvertisedIntegrationProfileIsExecuted(t *testing.T) {
	executed := map[string]string{}
	for _, execution := range Executions() {
		if execution.ID == "" || len(execution.Packages) == 0 {
			t.Errorf("incomplete execution: %+v", execution)
		}
		for _, profile := range execution.Profiles {
			if previous := executed[profile]; previous != "" {
				t.Errorf("profile %q is executed by both %s and %s", profile, previous, execution.ID)
			}
			executed[profile] = execution.ID
		}
	}
	for _, class := range conformance.All() {
		if executed[class.IntegrationProfile] == "" {
			t.Errorf("class %s maps to unexecuted integration profile %q", class.Key, class.IntegrationProfile)
		}
		if class.OfficialProfile != "" && class.DerivedProfile != "" {
			t.Errorf("class %s cannot be both official and derived", class.Key)
		}
	}
}
