# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

A Python scraper that pulls church service calendar data from the Finnish Orthodox Congregation in Sweden (ortodox-finsk.se).

## Development Setup

```bash
python3 -m venv venv
source venv/bin/activate
pip install requests beautifulsoup4
```

## Running the Scraper

```bash
source venv/bin/activate
python scrape_calendar.py
```

## Architecture

Single-module scraper with:
- `ChurchService` dataclass representing a calendar entry (date, day_of_week, service_name, location, time, occasion, notes)
- `fetch_calendar()` function that scrapes and parses the calendar page, returning a list of `ChurchService` objects
- The calendar HTML uses `section.calendar` containing `div.calendar-item` elements
