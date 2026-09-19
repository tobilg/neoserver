package sld

import "strings"

type ProcessInput string

const (
	ProcessVector ProcessInput = "vector"
	ProcessRaster ProcessInput = "raster"
)

type ProcessDefinition struct {
	Name        string
	Input       ProcessInput
	Output      ProcessInput
	Description string
}

var processRegistry = map[string]ProcessDefinition{
	"heatmap":                  {Name: "Heatmap", Input: ProcessVector, Output: ProcessRaster, Description: "Gaussian weighted point-density surface"},
	"contour":                  {Name: "Contour", Input: ProcessRaster, Output: ProcessVector, Description: "Marching-squares isolines"},
	"rasterize":                {Name: "Rasterize", Input: ProcessVector, Output: ProcessRaster, Description: "Vector portrayal rasterization"},
	"pointstacker":             {Name: "PointStacker", Input: ProcessVector, Output: ProcessVector, Description: "Pixel-grid point aggregation"},
	"groupcandidateselection":  {Name: "GroupCandidateSelection", Input: ProcessVector, Output: ProcessVector, Description: "One deterministic candidate per group"},
	"barnes":                   {Name: "Barnes", Input: ProcessVector, Output: ProcessRaster, Description: "Bounded Gaussian objective analysis"},
	"rasteraspointcollections": {Name: "RasterAsPointCollections", Input: ProcessRaster, Output: ProcessVector, Description: "Grid cells portrayed as point collections"},
	"rasteralgebra":            {Name: "RasterAlgebra", Input: ProcessRaster, Output: ProcessRaster, Description: "Bounded single-expression band algebra"},
}

func LookupProcess(name string) (ProcessDefinition, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if colon := strings.LastIndex(name, ":"); colon >= 0 {
		name = name[colon+1:]
	}
	value, ok := processRegistry[name]
	return value, ok
}
func RegisteredProcesses() []ProcessDefinition {
	names := []string{"heatmap", "contour", "rasterize", "pointstacker", "groupcandidateselection", "barnes", "rasteraspointcollections", "rasteralgebra"}
	result := make([]ProcessDefinition, 0, len(names))
	for _, name := range names {
		result = append(result, processRegistry[name])
	}
	return result
}
