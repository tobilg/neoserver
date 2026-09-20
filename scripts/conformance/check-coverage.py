#!/usr/bin/env python3
"""Fail on lost assertions or unreviewed skips; never rewrite ETS evidence."""
import argparse
from collections import Counter
import json
from pathlib import Path
import sys
import xml.etree.ElementTree as ET


def check(junit, policy):
    cases = list(ET.parse(junit).iter("testcase"))
    assertions = [case for case in cases if case.get("classname", "").startswith("assertion.")]
    errors = []
    if len(assertions) < policy["minimum_assertions"]:
        errors.append(f"Assertion count {len(assertions)} is below the reviewed minimum {policy['minimum_assertions']}")
    allowed = {(item["class"], item["name"]): item for item in policy["allowed_skips"]}
    skipped = Counter()
    for case in cases:
        skip = case.find("skipped")
        if skip is None:
            continue
        key = (case.get("classname", ""), case.get("name", ""))
        skipped[key] += 1
        requirement = allowed.get(key, {}).get("reason_contains")
        if requirement and requirement not in skip.get("message", "") + "".join(skip.itertext()):
            errors.append(f"Changed skip reason: {key[0]}.{key[1]}")
    for key, count in skipped.items():
        maximum = allowed.get(key, {}).get("maximum", 0)
        if count > maximum:
            errors.append(f"Unreviewed skips: {key[0]}.{key[1]}: {count}, allowed {maximum}")
    return {"passed": not errors, "assertions": len(assertions), "skipped_executions": sum(skipped.values()), "errors": errors}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("profile")
    parser.add_argument("directory", type=Path)
    args = parser.parse_args()
    policy_file = Path(__file__).resolve().parents[2] / "testing/officialets/coverage-policy.json"
    policy = json.loads(policy_file.read_text())
    if args.profile not in policy:
        parser.error(f"No reviewed coverage policy for {args.profile}")
    result = check(args.directory / "junit.xml", policy[args.profile])
    (args.directory / "coverage-check.json").write_text(json.dumps(result, indent=2) + "\n")
    print(f"{args.profile}: {result['assertions']} assertions, {result['skipped_executions']} skipped executions; coverage policy {'passed' if result['passed'] else 'FAILED'}")
    for error in result["errors"]:
        print(error, file=sys.stderr)
    return 0 if result["passed"] else 1


if __name__ == "__main__":
    sys.exit(main())
