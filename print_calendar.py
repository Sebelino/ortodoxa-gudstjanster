#!/usr/bin/env python3
"""
Print church service calendar from JSON file in a presentable format.
"""

import json
import sys


def main():
    input_file = "calendar.json"

    if len(sys.argv) > 1:
        input_file = sys.argv[1]

    try:
        with open(input_file, 'r', encoding='utf-8') as f:
            services = json.load(f)
    except FileNotFoundError:
        print(f"Error: {input_file} not found. Run fetch_calendar.py first.")
        sys.exit(1)

    if not services:
        print("No services found in calendar.")
        return

    print("Church Services Calendar")
    print("=" * 60)

    for service in services:
        print(f"\n{service['date']} ({service['day_of_week']})")
        print(f"  Service: {service['service_name']}")
        if service.get('occasion'):
            print(f"  Occasion: {service['occasion']}")
        if service.get('time'):
            print(f"  Time: {service['time']}")
        if service.get('location'):
            print(f"  Location: {service['location']}")
        if service.get('notes'):
            print(f"  Notes: {service['notes']}")

    print(f"\n{'=' * 60}")
    print(f"Total services: {len(services)}")


if __name__ == "__main__":
    main()
