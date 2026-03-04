#!/usr/bin/env python3
"""Compare the /services JSON endpoint with the /calendar.ics endpoint.

Usage:
    python3 scripts/compare-ics-json.py [BASE_URL]

Default BASE_URL is https://ortodoxagudstjanster.se
"""

import json
import re
import sys
import urllib.request


def fetch(url: str) -> str:
    with urllib.request.urlopen(url) as resp:
        return resp.read().decode("utf-8")


def parse_ics_events(ics_text: str) -> list[dict]:
    """Parse VEVENT blocks from an ICS string into dicts comparable with JSON services."""
    events = []
    blocks = ics_text.split("BEGIN:VEVENT")[1:]  # skip calendar header

    for block in blocks:
        block = block.split("END:VEVENT")[0]
        fields = parse_ics_fields(block)

        # Date and time from DTSTART
        dtstart = fields.get("DTSTART", "")
        date_str, time_str = "", None
        m = re.search(r"(\d{4})(\d{2})(\d{2})T(\d{2})(\d{2})", dtstart)
        if m:
            date_str = f"{m.group(1)}-{m.group(2)}-{m.group(3)}"
            time_str = f"{m.group(4)}:{m.group(5)}"
        else:
            # All-day event: VALUE=DATE:20260304
            m = re.search(r"(\d{4})(\d{2})(\d{2})", dtstart)
            if m:
                date_str = f"{m.group(1)}-{m.group(2)}-{m.group(3)}"

        summary = unescape_ics(fields.get("SUMMARY", ""))
        location = unescape_ics(fields.get("LOCATION", ""))
        parish = unescape_ics(fields.get("CATEGORIES", ""))
        description = unescape_ics(fields.get("DESCRIPTION", ""))

        language = extract_desc_field(description, "Språk")
        occasion = extract_desc_field(description, "Tillfälle")
        notes = extract_desc_field(description, "Info")
        source_url = extract_desc_field(description, "Källa")

        events.append({
            "date": date_str,
            "time": time_str,
            "service_name": summary,
            "location": location or None,
            "parish": parish,
            "language": language,
            "occasion": occasion,
            "notes": notes,
            "source_url": source_url or "",
        })

    return events


def parse_ics_fields(block: str) -> dict:
    """Parse ICS key-value fields, handling line continuations."""
    fields = {}
    # Unfold lines (RFC 5545: continuation lines start with space/tab)
    unfolded = re.sub(r"\r?\n[ \t]", "", block)
    for line in unfolded.split("\n"):
        line = line.strip("\r").strip()
        if not line:
            continue
        # Field name may include params like DTSTART;TZID=...
        m = re.match(r"^([A-Z][A-Z0-9_-]*)(;[^:]*)?:(.*)", line)
        if m:
            name = m.group(1)
            value = m.group(3)
            fields[name] = value
    return fields


def unescape_ics(s: str) -> str:
    s = s.replace("\\n", "\n")
    s = s.replace("\\,", ",")
    s = s.replace("\\;", ";")
    s = s.replace("\\\\", "\\")
    return s


def extract_desc_field(description: str, label: str) -> str | None:
    """Extract a labeled value from the ICS DESCRIPTION field.

    Values may span multiple lines. A field ends at the next known label or end of string.
    """
    known_labels = ["Församling", "Språk", "Tillfälle", "Info", "Källa"]
    # Build pattern: capture from "Label: " until next known label or end
    next_label_pattern = "|".join(re.escape(l) for l in known_labels if l != label)
    pattern = rf"{re.escape(label)}: (.*?)(?:\n(?:{next_label_pattern}): |$)"
    m = re.search(pattern, description, re.DOTALL)
    return m.group(1).rstrip("\n") if m else None


def normalize(value) -> str:
    """Normalize a value for comparison."""
    if value is None:
        return ""
    return str(value)


def compare(json_services: list[dict], ics_events: list[dict]) -> list[str]:
    """Compare JSON services with ICS events and return a list of mismatch descriptions."""
    issues = []

    if len(json_services) != len(ics_events):
        issues.append(f"Count mismatch: JSON has {len(json_services)} events, ICS has {len(ics_events)}")

    compare_fields = [
        "date",
        "service_name",
        "location",
        "parish",
        "language",
        "occasion",
        "notes",
    ]

    for i, (svc, ics) in enumerate(zip(json_services, ics_events)):
        diffs = []

        # Time: ICS only has start time, so extract start from JSON time range
        json_time = normalize(svc.get("time"))
        ics_time = normalize(ics.get("time"))
        json_start = json_time.split(" - ")[0].split(" – ")[0].strip()
        if json_start != ics_time:
            diffs.append(f"  time: JSON={json_time!r} (start={json_start!r})  ICS={ics_time!r}")

        for field in compare_fields:
            json_val = normalize(svc.get(field))
            ics_val = normalize(ics.get(field))
            if json_val != ics_val:
                diffs.append(f"  {field}: JSON={json_val!r}  ICS={ics_val!r}")

        # source_url: ICS falls back to source name when source_url is empty
        json_source_url = svc.get("source_url") or svc.get("source", "")
        ics_source_url = ics.get("source_url") or ""
        if normalize(json_source_url) != normalize(ics_source_url):
            diffs.append(f"  source_url: JSON={json_source_url!r}  ICS={ics_source_url!r}")
        if diffs:
            name = svc.get("service_name", "")[:50]
            issues.append(f"Event {i} ({svc.get('date')} {name}):\n" + "\n".join(diffs))

    return issues


def main():
    base_url = sys.argv[1] if len(sys.argv) > 1 else "https://ortodoxagudstjanster.se"
    base_url = base_url.rstrip("/")

    print(f"Fetching from {base_url} ...")
    json_text = fetch(f"{base_url}/services")
    ics_text = fetch(f"{base_url}/calendar.ics")

    json_services = json.loads(json_text)
    ics_events = parse_ics_events(ics_text)

    print(f"JSON services: {len(json_services)}")
    print(f"ICS events:    {len(ics_events)}")

    issues = compare(json_services, ics_events)

    if not issues:
        print("\nAll events match perfectly!")
        sys.exit(0)
    else:
        print(f"\nFound {len(issues)} issue(s):\n")
        for issue in issues:
            print(issue)
            print()
        sys.exit(1)


if __name__ == "__main__":
    main()
