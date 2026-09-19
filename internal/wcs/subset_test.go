package wcs

import (
	"github.com/tobilg/neoserver/internal/datasource"
	"testing"
)

func TestParseSubsets(t *testing.T) {
	info := &datasource.CoverageInfo{AxisLabels: [2]string{"x", "y"}, Width: 10, Height: 10, OriginX: 0, OriginY: 10, ResolutionX: 1, ResolutionY: -1}
	tests := []struct {
		name      string
		values    []string
		want      datasource.CoverageWindow
		errorCode string
	}{{"full", nil, datasource.CoverageWindow{Width: 10, Height: 10}, ""}, {"trim", []string{"x(1.5,3.5)", "y(7.5,8.5)"}, datasource.CoverageWindow{XOff: 1, YOff: 1, Width: 3, Height: 2}, ""}, {"slice", []string{"x(2.5)"}, datasource.CoverageWindow{XOff: 2, Width: 1, Height: 10}, ""}, {"duplicate", []string{"x(1.5)", "X(2.5)"}, datasource.CoverageWindow{}, "InvalidAxisLabel"}, {"off grid slice", []string{"x(2.6)"}, datasource.CoverageWindow{}, "InvalidSubsetting"}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSubsets(tt.values, info)
			if tt.errorCode != "" {
				if err == nil || err.Code != tt.errorCode {
					t.Fatalf("err=%v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %+v want %+v", got, tt.want)
			}
		})
	}
}
