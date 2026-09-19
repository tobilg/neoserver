package query

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

type BBox struct {
	MinX float64
	MinY float64
	MaxX float64
	MaxY float64
}

func ParseBBox(v string) (BBox, error) {
	parts := strings.Split(v, ",")
	if len(parts) != 4 {
		return BBox{}, fmt.Errorf("bbox must have 4 comma-separated numbers: minx,miny,maxx,maxy")
	}
	num := make([]float64, 4)
	for i := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(parts[i]), 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return BBox{}, fmt.Errorf("bbox value %q is not a number", parts[i])
		}
		num[i] = f
	}
	// A CRS84 bbox may cross the antimeridian, in which case west is greater
	// than east (for example 177,65,-177,70). The caller knows the bbox CRS
	// and is responsible for accepting that ordering only for CRS84.
	if num[1] > num[3] {
		return BBox{}, fmt.Errorf("bbox south value must be <= north value")
	}
	return BBox{MinX: num[0], MinY: num[1], MaxX: num[2], MaxY: num[3]}, nil
}
