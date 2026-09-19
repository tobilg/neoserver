# WCS protocol integration mapping

These are native black-box regression tests against a live neoserver fixture.
They are not an official ETS or an OGC certification result.

| Test | Requirement area |
| --- | --- |
| `TestWCS20CoreAndKVP` | WCS 2.0 Core and GET/KVP binding |
| `TestWCS20CoverageRepresentations` | CIS 1.0 GML, rectified-grid and grid coverages |
| `TestWCS20MultipartAndErrors` | Multipart representation and OWS exception behavior |
| `TestWCS20Extensions` | Range subsetting, scaling, CRS, and Interpolation 1.0 |
| `TestWCS21CoreAndKVP` | WCS 2.1 Core, GET/KVP, CIS 1.1, and GeoTIFF retrieval |
| `TestWCS21ExtensionDeclarationsAndExecution` | Interpolation 1.1 declaration and nearest/linear execution |

The canonical class-to-execution mapping is validated by the registry under
`testing/protocol` and the production catalog under `internal/conformance`.
