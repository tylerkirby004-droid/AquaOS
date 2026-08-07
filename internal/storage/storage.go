// Package storage defines durable storage independently of InfluxDB or another backend.
package storage

import "github.com/tylerkirby004-droid/aquaos/internal/health"

// Storage is the lifecycle boundary for future durable persistence adapters.
type Storage interface{ health.Component }
