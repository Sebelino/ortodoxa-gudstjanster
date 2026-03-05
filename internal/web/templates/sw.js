const CACHE_NAME = 'ortodoxa-v1';
const SHELL = ['/favicon.svg', '/manifest.json'];

self.addEventListener('install', event => {
    event.waitUntil(
        caches.open(CACHE_NAME).then(cache => cache.addAll(SHELL))
    );
    self.skipWaiting();
});

self.addEventListener('activate', event => {
    event.waitUntil(
        caches.keys().then(keys =>
            Promise.all(keys.filter(k => k !== CACHE_NAME).map(k => caches.delete(k)))
        )
    );
    self.clients.claim();
});

self.addEventListener('fetch', event => {
    const url = new URL(event.request.url);

    // Network-first for API and dynamic content
    if (url.pathname === '/services' || url.pathname === '/calendar.ics' || url.pathname === '/last-updated') {
        event.respondWith(fetch(event.request).catch(() => caches.match(event.request)));
        return;
    }

    // Cache-first for static assets
    if (url.pathname === '/favicon.svg' || url.pathname === '/manifest.json') {
        event.respondWith(
            caches.match(event.request).then(cached => cached || fetch(event.request))
        );
        return;
    }

    // Network-first for everything else (HTML)
    event.respondWith(
        fetch(event.request).catch(() => caches.match(event.request))
    );
});
