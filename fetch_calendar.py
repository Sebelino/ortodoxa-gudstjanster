#!/usr/bin/env python3
"""
Scrape church service calendar from ortodox-finsk.se and save to JSON.
"""

import json
import requests
from bs4 import BeautifulSoup
import re
from dataclasses import dataclass, asdict


@dataclass
class ChurchService:
    date: str
    day_of_week: str
    service_name: str
    location: str | None
    time: str | None
    occasion: str | None
    notes: str | None


def fetch_calendar(url: str = "https://www.ortodox-finsk.se/kalender/") -> list[ChurchService]:
    """Fetch and parse church services from the calendar page."""
    response = requests.get(url)
    response.raise_for_status()
    response.encoding = 'utf-8'

    soup = BeautifulSoup(response.text, 'html.parser')
    services = []

    # Find the calendar section
    calendar = soup.find('section', class_='calendar')
    if not calendar:
        return services

    # Find all calendar items
    items = calendar.find_all('div', class_='calendar-item')

    for item in items:
        # Extract date from meta div
        meta = item.find('div', class_='meta')
        if not meta:
            continue

        meta_text = meta.get_text(strip=True)
        # Parse date: "2026-02-20 | Fredag"
        date_match = re.search(r'(\d{4}-\d{2}-\d{2})\s*\|\s*(\w+)', meta_text)
        if not date_match:
            continue

        date = date_match.group(1)
        day_of_week = date_match.group(2)

        # Extract service name from h3
        content_div = item.find('div', class_='calendar-item-content')
        if not content_div:
            continue

        h3 = content_div.find('h3')
        service_name = h3.get_text(strip=True) if h3 else "Unknown"

        # Extract location, time, occasion and notes from the content div
        location = None
        time = None
        occasion = None
        notes = []

        # Get the inner div with details
        details_div = content_div.find('div')
        if details_div:
            # Get the full text content to parse
            full_text = details_div.decode_contents()

            # Extract location
            loc_match = re.search(r'<strong>\s*Plats:\s*</strong>\s*([^<]+)', full_text)
            if loc_match:
                location = loc_match.group(1).strip()

            # Extract time
            time_match = re.search(r'<strong>\s*Tid:\s*</strong>\s*([^<]+)', full_text)
            if time_match:
                time = time_match.group(1).strip()

            # Extract occasion (first strong tag that's not Plats/Tid)
            first_strong = details_div.find('strong')
            if first_strong:
                strong_text = first_strong.get_text(strip=True)
                if strong_text and strong_text not in ['Plats:', 'Tid:']:
                    occasion = strong_text

            # Extract notes from <p> tags
            for p in details_div.find_all('p'):
                p_text = p.get_text(strip=True)
                if p_text:
                    notes.append(p_text)

        services.append(ChurchService(
            date=date,
            day_of_week=day_of_week,
            service_name=service_name,
            location=location,
            time=time,
            occasion=occasion,
            notes='\n'.join(notes) if notes else None
        ))

    return services


def main():
    output_file = "calendar.json"

    print("Fetching church calendar from ortodox-finsk.se...")
    services = fetch_calendar()

    if not services:
        print("No services found.")
        return

    # Convert to list of dicts and save as JSON
    data = [asdict(service) for service in services]

    with open(output_file, 'w', encoding='utf-8') as f:
        json.dump(data, f, ensure_ascii=False, indent=2)

    print(f"Saved {len(services)} services to {output_file}")


if __name__ == "__main__":
    main()
