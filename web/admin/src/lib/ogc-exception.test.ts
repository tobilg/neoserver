import { describe, expect, it } from "vitest";
import { ogcExceptionMessage } from "./ogc-exception";

describe("ogcExceptionMessage", () => {
  it("extracts WMS service exception text", () => {
    expect(
      ogcExceptionMessage(
        `<?xml version="1.0"?><ServiceExceptionReport version="1.3.0"><ServiceException code="LayerNotDefined">Layer not found: a &amp; b</ServiceException></ServiceExceptionReport>`,
      ),
    ).toBe("Layer not found: a & b");
  });

  it("extracts OWS exception text", () => {
    expect(
      ogcExceptionMessage(
        `<ows:ExceptionReport><ows:Exception exceptionCode="X"><ows:ExceptionText>First</ows:ExceptionText></ows:Exception><ows:Exception><ows:ExceptionText>Second</ows:ExceptionText></ows:Exception></ows:ExceptionReport>`,
      ),
    ).toBe("First\nSecond");
  });

  it("leaves other bodies alone", () => {
    expect(ogcExceptionMessage("Bad gateway")).toBe("Bad gateway");
  });
});
