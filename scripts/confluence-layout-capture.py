#!/usr/bin/env python3
# Copyright 2026 Marcelo Cantos
# SPDX-License-Identifier: Apache-2.0

"""Capture the rendered geometry of a published Confluence test page.

This gathers evidence for a later visual verdict; it does not claim that a
page looks good. Keep captures outside the repository because they may include
private page content and signed media URLs.
"""

import argparse
import json
import tempfile
from pathlib import Path
from urllib.parse import urlparse

from playwright.sync_api import sync_playwright


VIEWPORTS = (("desktop", 1440, 900), ("narrow", 900, 900))
REPO_ROOT = Path(__file__).resolve().parents[1]
PAGE_TIMEOUT_MS = 60_000
MARKER_TIMEOUT_MS = 45_000
MAX_SCROLL_STEPS = 100
SCROLL_FRACTION = 0.8
SCROLL_PAUSE_MS = 100
IMAGE_DECODE_TIMEOUT_MS = 10_000
MAX_STABILITY_SAMPLES = 40
REQUIRED_STABLE_SAMPLES = 3
STABILITY_SAMPLE_MS = 250
TOP_SETTLE_MS = 350


def capture_page(browser, url, marker, content_selector, storage_state, name, width, height, out, self_test):
    context_args = {"viewport": {"width": width, "height": height}, "device_scale_factor": 1}
    if storage_state:
        context_args["storage_state"] = str(storage_state)
    context = browser.new_context(**context_args)
    page = context.new_page()
    page_errors = []
    request_failures = []
    page.on("pageerror", lambda error: page_errors.append(str(error)))
    page.on(
        "requestfailed",
        lambda request: request_failures.append(
            {"method": request.method, "resource_type": request.resource_type, "failure": request.failure}
        ),
    )

    response = page.goto(url, wait_until="domcontentloaded", timeout=PAGE_TIMEOUT_MS)
    page.get_by_text(marker, exact=True).first.wait_for(state="visible", timeout=MARKER_TIMEOUT_MS)
    page.evaluate("() => document.fonts.ready")

    # Confluence may lazy-load media only when it approaches the viewport.
    # Visit the full document before measuring or taking a full-page image.
    for _ in range(MAX_SCROLL_STEPS):
        at_bottom = page.evaluate(
            """scrollFraction => {
                const scroller = document.scrollingElement;
                const next = Math.min(scroller.scrollTop + innerHeight * scrollFraction, scroller.scrollHeight);
                scroller.scrollTop = next;
                return scroller.scrollTop + innerHeight >= scroller.scrollHeight - 2;
            }""",
            SCROLL_FRACTION,
        )
        page.wait_for_timeout(SCROLL_PAUSE_MS)
        if at_bottom:
            break

    # Decode loaded images; report incomplete ones rather than hiding them.
    page.evaluate(
        """async timeout => {
            await Promise.race([
                Promise.all([...document.images].map(image => image.decode().catch(() => {}))),
                new Promise(resolve => setTimeout(resolve, timeout))
            ]);
            await document.fonts.ready;
            await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
        }""",
        IMAGE_DECODE_TIMEOUT_MS,
    )
    stable_samples = 0
    previous_shape = None
    for _ in range(MAX_STABILITY_SAMPLES):
        shape = page.evaluate(
            """() => JSON.stringify({
                height: document.scrollingElement.scrollHeight,
                width: document.scrollingElement.scrollWidth,
                images: [...document.images].map(image =>
                    [image.complete, image.naturalWidth, Math.round(image.getBoundingClientRect().width)]),
                frames: [...document.querySelectorAll('iframe')].map(frame =>
                    [Math.round(frame.getBoundingClientRect().width), Math.round(frame.getBoundingClientRect().height)])
            })"""
        )
        stable_samples = stable_samples + 1 if shape == previous_shape else 0
        if stable_samples >= REQUIRED_STABLE_SAMPLES:
            break
        previous_shape = shape
        page.wait_for_timeout(STABILITY_SAMPLE_MS)
    page.evaluate("() => { document.scrollingElement.scrollTop = 0; }")
    page.wait_for_timeout(TOP_SETTLE_MS)

    geometry = page.evaluate(
        """selector => {
            const root = document.querySelector(selector);
            if (!root) throw new Error(`content selector not found: ${selector}`);
            const box = element => {
                const rect = element.getBoundingClientRect();
                return {x: rect.x, y: rect.y, width: rect.width, height: rect.height,
                        scrollWidth: element.scrollWidth, clientWidth: element.clientWidth};
            };
            const clean = value => (value || '').replace(/\\s+/g, ' ').trim().slice(0, 160);
            return {
                title: document.title,
                viewport: {width: innerWidth, height: innerHeight},
                document: {width: document.scrollingElement.scrollWidth,
                           height: document.scrollingElement.scrollHeight},
                content: box(root),
                headings: [...root.querySelectorAll('h1,h2,h3,h4,h5,h6')].map(element => ({
                    level: element.tagName, text: clean(element.textContent), box: box(element)
                })),
                tables: [...root.querySelectorAll('table,[role="table"]')].map((table, index) => {
                    const rows = table.rows ? [...table.rows] : [...table.querySelectorAll('[role="row"]')];
                    return {index, box: box(table),
                        rows: rows.map(row => {
                            const cells = row.cells ? [...row.cells] :
                                [...row.querySelectorAll('[role="cell"],[role="columnheader"]')];
                            return cells.map(cell => ({text: clean(cell.textContent), box: box(cell)}));
                        })};
                }),
                images: [...root.querySelectorAll('img')].map((element, index) => ({
                    index, alt: element.alt, loaded: element.complete && element.naturalWidth > 0,
                    naturalWidth: element.naturalWidth, naturalHeight: element.naturalHeight,
                    box: box(element)
                })),
                svgs: [...root.querySelectorAll('svg')].map((element, index) => ({
                    index, label: element.getAttribute('aria-label'),
                    viewBox: element.getAttribute('viewBox'), box: box(element)
                })),
                roleImages: [...root.querySelectorAll('[role="img"]')].map((element, index) => ({
                    index, label: element.getAttribute('aria-label'), box: box(element)
                })),
                frames: [...root.querySelectorAll('iframe')].map((element, index) => ({
                    index, title: element.title, box: box(element)
                }))
            };
        }""",
        content_selector,
    )

    screenshot = out / f"{name}.png"
    page.screenshot(path=str(screenshot), full_page=True, animations="disabled")
    result = {
        "url": page.url,
        "http_status": response.status if response else None,
        "marker": marker,
        "self_test": self_test,
        "render_stable": stable_samples >= REQUIRED_STABLE_SAMPLES,
        "screenshot": str(screenshot),
        "geometry": geometry,
        "page_errors": page_errors,
        "request_failures": request_failures,
    }
    (out / f"{name}.json").write_text(json.dumps(result, indent=2) + "\n", encoding="utf-8")
    context.close()
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--url", required=True, help="Published Confluence page URL")
    parser.add_argument("--marker", required=True, help="Exact visible document heading")
    parser.add_argument("--content-selector", default="main", help="CSS selector for published page content")
    parser.add_argument("--storage-state", type=Path, help="Playwright auth state, stored outside this repository")
    parser.add_argument("--out", type=Path, help="Absolute scratch directory; default is a new temporary directory")
    parser.add_argument("--self-test", action="store_true", help="Allow a local file URL to check the capture machinery only")
    args = parser.parse_args()

    parsed = urlparse(args.url)
    if args.self_test:
        if parsed.scheme != "file" or not parsed.path:
            parser.error("--self-test requires a local file URL")
    elif parsed.scheme != "https" or not parsed.netloc:
        parser.error("--url must be an HTTPS published Confluence page URL")
    if args.storage_state and not args.storage_state.is_file():
        parser.error("--storage-state does not exist")

    out = args.out or Path(tempfile.mkdtemp(prefix="vellum-confluence-"))
    if not out.is_absolute():
        parser.error("--out must be an absolute scratch path")
    out = out.resolve()
    if out == REPO_ROOT or REPO_ROOT in out.parents:
        parser.error("captures must be written outside the repository")
    out.mkdir(parents=True, exist_ok=True)

    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True)
        try:
            for name, width, height in VIEWPORTS:
                result = capture_page(
                    browser, args.url, args.marker, args.content_selector,
                    args.storage_state, name, width, height, out, args.self_test,
                )
                print(f"{name}: {result['screenshot']} and {out / (name + '.json')}")
        finally:
            browser.close()


if __name__ == "__main__":
    main()
