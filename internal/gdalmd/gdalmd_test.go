package gdalmd_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/internal/gdalmd"
)

// Keep this test independent of other datasource packages so it verifies that
// gdalmd brings its own GDAL link dependency through godal.
func TestStandaloneMultidimensionalRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "grid.vrt")
	const vrt = `<VRTDataset>
  <Group name="/">
    <Dimension name="sample" size="3"/>
    <Array name="values">
      <DataType>Float64</DataType>
      <DimensionRef ref="sample"/>
      <RegularlySpacedValues start="2" increment="3"/>
    </Array>
  </Group>
</VRTDataset>`
	if err := os.WriteFile(path, []byte(vrt), 0o600); err != nil {
		t.Fatal(err)
	}
	dataset, err := gdalmd.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := dataset.Close(); err != nil {
			t.Error(err)
		}
	})
	names, err := dataset.ArrayNames()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(names, []string{"/values"}) {
		t.Fatalf("array names = %v, want [/values]", names)
	}
	values, err := dataset.ReadFloat64("/values", []uint64{0}, []uint64{3})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(values, []float64{2, 5, 8}) {
		t.Fatalf("values = %v, want [2 5 8]", values)
	}
}

func TestGDALHeadersDoNotDuplicateLinkLibraries(t *testing.T) {
	pkgConfig := os.Getenv("PKG_CONFIG")
	if pkgConfig == "" {
		pkgConfig = "pkg-config"
	}
	output, err := exec.Command(pkgConfig, "--libs", "./gdal-headers.pc").CombinedOutput()
	if err != nil {
		t.Fatalf("pkg-config: %v: %s", err, output)
	}
	if flags := strings.TrimSpace(string(output)); flags != "" {
		t.Fatalf("GDAL headers must not add linker flags already supplied by godal: %s", flags)
	}
}
