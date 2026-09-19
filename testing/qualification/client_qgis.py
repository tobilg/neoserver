"""Real QGIS provider acceptance; runs only in the disposable qualification network."""
import json
import os
import gc
import traceback
import platform
import xml.etree.ElementTree as ET
from pathlib import Path
from qgis.core import (
    Qgis, QgsApplication, QgsNetworkAccessManager, QgsVectorLayer, QgsRasterLayer,
    QgsFeature, QgsGeometry, QgsPointXY, QgsRectangle, QgsCoordinateReferenceSystem,
    QgsMapSettings, QgsMapRendererSequentialJob, QgsVectorFileWriter, QgsProject,
    QgsProviderRegistry, QgsDataSourceUri, QgsAuthMethodConfig,
)
from qgis.PyQt.QtCore import QSize, QUrl
from qgis.PyQt.QtGui import QColor
from qgis.PyQt.QtNetwork import QNetworkRequest

# The pinned image builds QGIS under /usr/local, while exposing its Python
# bindings under /usr. Set the native prefix before provider discovery.
QgsApplication.setPrefixPath("/usr/local", True)
app = QgsApplication([], False)
app.initQgis()
base = "http://server:9000/workspaces/qualification"
token = os.environ["QUAL_TOKEN"]
report = {"client": Qgis.QGIS_VERSION, "platform": platform.platform(), "checks": [], "passed": False}
messages = []
QgsApplication.messageLog().messageReceived.connect(
    lambda message, tag, level: messages.append({"tag": tag, "message": message})
)
assert Qgis.QGIS_VERSION.startswith("3.44.14"), report
assert {"WFS", "wms", "gdal", "ogr"}.issubset(QgsProviderRegistry.instance().providerList())
QgsNetworkAccessManager.setTimeout(30000)
auth = QgsApplication.authManager()
assert auth.setMasterPassword("disposable-qualification-auth", True)
auth_config = QgsAuthMethodConfig()
auth_config.setName("Disposable qualification bearer")
auth_config.setMethod("APIHeader")
auth_config.setConfig("Authorization", "Bearer " + token)
assert auth.storeAuthenticationConfig(auth_config)

def connection(endpoint, **params):
    uri = QgsDataSourceUri()
    uri.setParam("url", base + endpoint)
    for key, value in params.items():
        uri.setParam(key, value)
    uri.setAuthConfigId(auth_config.id())
    return uri

def render(layer, name, extent, crs="EPSG:4326"):
    assert layer.isValid(), name + ": " + layer.error().summary()
    settings = QgsMapSettings()
    settings.setLayers([layer])
    settings.setDestinationCrs(QgsCoordinateReferenceSystem(crs))
    settings.setExtent(QgsRectangle(*extent))
    settings.setOutputSize(QSize(256, 256))
    settings.setBackgroundColor(QColor("white"))
    job = QgsMapRendererSequentialJob(settings)
    job.start()
    job.waitForFinished()
    assert not job.errors(), [(e.message) for e in job.errors()]
    image = job.renderedImage()
    assert not image.isNull()
    assert any(image.pixelColor(x, y) != QColor("white") for x in range(256) for y in range(256)), name + " is blank"
    image.save("/tmp/" + name + ".png")
    report["checks"].append(name + ": nonblank provider render")

def run_checks():
    expired = QgsAuthMethodConfig()
    expired.setName("Expired fixture credential")
    expired.setMethod("APIHeader")
    expired.setConfig("Authorization", "Bearer " + os.environ["QUAL_EXPIRED_TOKEN"])
    assert auth.storeAuthenticationConfig(expired)
    capabilities = QNetworkRequest(QUrl(base + "/wfs?SERVICE=WFS&REQUEST=GetCapabilities&VERSION=2.0.0"))
    denied = QgsNetworkAccessManager.blockingGet(capabilities, expired.id(), True)
    assert denied.attribute(QNetworkRequest.HttpStatusCodeAttribute) == 401
    connected = QgsNetworkAccessManager.blockingGet(capabilities, auth_config.id(), True)
    assert connected.attribute(QNetworkRequest.HttpStatusCodeAttribute) == 200
    report["checks"].append("Expired bearer credential denied; valid saved APIHeader connection reconnects")
    print("Checking QGIS WFS discovery and paging", flush=True)
    uri = connection("/wfs", typename="points", version="2.0.0", srsname="EPSG:4326", pagingEnabled="true", pageSize="37")
    vector = QgsVectorLayer(uri.uri(False), "points", "WFS")
    assert vector.isValid(), "WFS layer invalid: " + vector.error().summary() + "; " + json.dumps(messages)
    features = list(vector.getFeatures())
    assert len(features) == 10000, len(features)
    control = next(f for f in features if f["name"] == "point-1")
    point = control.geometry().asPoint()
    assert abs(point.x() - 7) < 1e-8 and abs(point.y() - 51) < 1e-8, (point.x(), point.y())
    report["checks"].append("WFS 2.0 authenticated discovery, 37-feature paging and EPSG:4326 axis control")
    options = QgsVectorFileWriter.SaveVectorOptions()
    options.driverName = "GPKG"
    saved = QgsVectorFileWriter.writeAsVectorFormatV3(vector, "/tmp/export.gpkg", QgsProject.instance().transformContext(), options)
    assert saved[0] == QgsVectorFileWriter.NoError, saved
    exported = QgsVectorLayer("/tmp/export.gpkg", "export", "ogr")
    assert exported.featureCount() == 10000
    report["checks"].append("WFS export to GeoPackage retains all 10000 features")
    print("Checking QGIS WMS and WMTS rendering", flush=True)

    # Non-symmetric world controls test the native WMS provider's axis handling.
    for crs, extent in [("EPSG:4326", [6.99, 50.99, 7.11, 51.11]), ("EPSG:3857", [778000, 6619000, 792000, 6640000])]:
        uri = connection("/wms", layers="points", styles="", format="image/png", crs=crs, version="1.3.0")
        layer = QgsRasterLayer(bytes(uri.encodedUri()).decode(), "map", "wms")
        render(layer, "wms-" + crs.replace(":", "-"), extent, crs)
    uri = connection("/wms", layers="coverage", styles="", format="image/png", crs="EPSG:4326", version="1.3.0")
    raster = QgsRasterLayer(bytes(uri.encodedUri()).decode(), "raster", "wms")
    render(raster, "wms-raster", [10, 54, 11, 55])
    uri = connection("/wmts?SERVICE=WMTS&REQUEST=GetCapabilities&VERSION=1.0.0", layers="points", styles="default", format="image/png", crs="EPSG:3857", tileMatrixSet="WebMercatorQuad")
    wmts = QgsRasterLayer(bytes(uri.encodedUri()).decode(), "tiles", "wms")
    render(wmts, "wmts", [778000, 6619000, 792000, 6640000], "EPSG:3857")

    # This exercises the WCS result through QGIS's GDAL raster provider, not
    # QGIS's separate native WCS connection/provider.
    url = base + "/wcs?SERVICE=WCS&VERSION=2.0.1&REQUEST=GetCoverage&COVERAGEID=coverage&FORMAT=image/tiff&SUBSET=x(10.1,10.8)&SUBSET=y(54.1,54.8)"
    reply = QgsNetworkAccessManager.blockingGet(QNetworkRequest(QUrl(url)), auth_config.id(), True)
    assert reply.attribute(QNetworkRequest.HttpStatusCodeAttribute) == 200
    Path("/tmp/coverage.tif").write_bytes(bytes(reply.content()))
    subset = QgsRasterLayer("/tmp/coverage.tif", "WCS subset", "gdal")
    assert subset.isValid() and subset.bandCount() == 3
    assert subset.crs().authid() == "EPSG:4326"
    assert subset.width() == 7 and subset.height() == 7
    extent = subset.extent()
    assert abs(extent.xMinimum() - 10.1) < 1e-8 and abs(extent.yMinimum() - 54.1) < 1e-8
    assert abs(extent.xMaximum() - 10.8) < 1e-8 and abs(extent.yMaximum() - 54.8) < 1e-8
    provider = subset.dataProvider()
    assert provider.sourceNoDataValue(1) == -9999
    value, valid = provider.sample(QgsPointXY(10.45, 54.45), 2)
    assert valid and value == 100
    report["checks"].append("WCS 2.0.1 subset read by QGIS GDAL: 7x7 pixels, three bands, CRS, extent, nodata and sample value")

    print("Checking QGIS WFS-T", flush=True)
    assert vector.startEditing(), "QGIS does not enable WFS editing"
    added = QgsFeature(vector.fields())
    added.setAttribute("name", "qgis-created")
    added.setGeometry(QgsGeometry.fromPointXY(QgsPointXY(9, 49)))
    assert vector.addFeature(added)
    transaction_versions = []
    def observe_transaction(request, operation, data):
        if request.url().path().endswith("/wfs") and data:
            root = ET.fromstring(bytes(data))
            if root.tag.endswith("Transaction"):
                transaction_versions.append(root.attrib.get("version"))
        return operation, data
    observer = QgsNetworkAccessManager.setAdvancedRequestPreprocessor(observe_transaction)
    try:
        committed = vector.commitChanges()
    finally:
        QgsNetworkAccessManager.removeAdvancedRequestPreprocessor(observer)
    # QGIS 3.44.14 createTransactionElement downgrades WFS 2.0 connections to
    # WFS 1.0 writes. Preserve this explicit interoperability boundary; a
    # rejected legacy transaction is not evidence of working WFS-T in QGIS.
    assert transaction_versions == ["1.0.0"], transaction_versions
    report["wfs_transaction_version"] = transaction_versions[0]
    assert not committed, "a WFS 1.0 transaction must not be accepted as WFS 2.0"
    vector.rollBack()
    report["unsupported"] = {
        "QGIS WFS-T": "Client emits WFS 1.0.0; neoserver accepts only WFS 2.0.0. Use a WFS 2.0 transaction client."
    }
    report["checks"].append("QGIS legacy WFS transaction rejected; editing is NOT qualified")

try:
    run_checks()
    report["passed"] = True
except Exception as error:
    report["error"] = repr(error)
    report["traceback"] = traceback.format_exc()
    report["messages"] = messages
finally:
    # QGIS provider objects must be destroyed before application teardown.
    gc.collect()
    app.exitQgis()
    print("NEOSERVER_QGIS_RESULT=" + json.dumps(report).replace(token, "[redacted]").replace(os.environ["QUAL_EXPIRED_TOKEN"], "[redacted]"), flush=True)
raise SystemExit(0 if report["passed"] else 1)
