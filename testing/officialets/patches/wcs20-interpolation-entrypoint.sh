#!/bin/sh
set -eu

# ets-wcs20 1.21 passes interpolation identifiers through xsl:select as bare
# XPath expressions. TEAM Engine aborts those tests with CTL result -1 before
# issuing a request. Quote the identifiers as XPath strings so the published
# assertions can execute without changing their semantics.
suite_file=/root/te_base/scripts/wcs/2.0.1/ctl/ext_get-int.xml
bare_pattern='select="http://www.opengis.net/def/interpolation/OGC/1/'
quoted_pattern='select="&apos;http://www.opengis.net/def/interpolation/OGC/1/'
expected=18
found="$(grep -c "$bare_pattern" "$suite_file")"
if [ "$found" -ne "$expected" ]; then
    echo "unexpected ets-wcs20 interpolation patch input: found $found bare identifiers, want $expected" >&2
    exit 1
fi

sed -Ei 's#select="(http://www\.opengis\.net/def/interpolation/OGC/1/[^"]+)"#select="\&apos;\1\&apos;"#g' "$suite_file"

remaining="$(grep -c "$bare_pattern" "$suite_file" || true)"
if [ "$remaining" -ne 0 ]; then
    echo "ets-wcs20 interpolation patch left $remaining bare identifiers" >&2
    exit 1
fi

quoted="$(grep -c "$quoted_pattern" "$suite_file")"
if [ "$quoted" -ne "$expected" ]; then
    echo "ets-wcs20 interpolation patch produced $quoted quoted identifiers, want $expected" >&2
    exit 1
fi

exec "$@"
