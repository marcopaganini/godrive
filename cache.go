package gdrive_path

// This file is part of the gdrive_path Go library
//
// (C) Sep/2014 by Marco Paganini <paganini@paganini.net>

import "time"

const (
	CACHE_TTL_SECONDS = 60
)

// Object cache
type objCache struct {
	obj       interface{}
	timestamp time.Time
}

// Add/replace object in the cache using 'drivePath' as a key.
func cacheAdd(cache *map[string]*objCache, drivePath string, obj interface{}) {
	item := &objCache{obj, time.Now()}
	m := *cache
	m[drivePath] = item
}

// Retrieve object from the cache using 'drivePath' as a key.
// Returns an *interface{} object or nil if not found or expired.
func cacheGet(cache *map[string]*objCache, drivePath string) interface{} {
	m := *cache
	item, ok := m[drivePath]
	if ok {
		if time.Now().After(item.timestamp.Add(CACHE_TTL_SECONDS * time.Second)) {
			cacheDel(cache, drivePath)
			return nil
		} else {
			return item.obj
		}
	}

	return nil
}

// Remove object from the cache using 'drivePath' as a key.
func cacheDel(cache *map[string]*objCache, drivePath string) {
	delete(*cache, drivePath)
}
