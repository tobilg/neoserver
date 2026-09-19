# Native runtime notices

These files accompany the libraries copied from the digest-pinned OSGeo GDAL
builder. Ubuntu-managed dependencies retain their own package copyright files.

| Library | Upstream license source |
| --- | --- |
| GDAL 3.13.3 (`libgdal.so.39`, plugins, tools, data) | https://github.com/OSGeo/gdal/blob/v3.13.3/LICENSE.TXT |
| PROJ 9.8.1 (`libinternalproj.so.25`, tools, data) | https://github.com/OSGeo/PROJ/blob/9.8.1/COPYING |
| JPEG XL (`libjxl.so.0.13`, `libjxl_cms.so.0.13`) | https://github.com/libjxl/libjxl/blob/main/LICENSE |
| QB3 (`libQB3.so`) | https://github.com/lucianpls/QB3/blob/master/LICENSE |

The upstream GDAL build recipe builds JPEG XL and QB3 from their development
branches. The image digest, and the file checksums in
`/usr/share/neoserver/runtime-libraries.json`, identify the exact binaries;
they are not represented as Ubuntu packages. Review these dependencies when
changing the pinned builder. The scanner's dpkg inventory does not cover them.
