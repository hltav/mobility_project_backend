package cache

import "time"

const (
	// TTLLines define o tempo de vida das linhas de ônibus no cache.
	// Linhas são praticamente imutáveis — reavaliado a cada 60 dias.
	TTLLines = 60 * 24 * time.Hour

	// TTLStops define o tempo de vida das paradas no cache.
	// Mudanças geralmente têm 30 dias de aviso prévio.
	TTLStops = 30 * 24 * time.Hour

	// TTLBuses define o tempo de vida das posições dos ônibus no cache.
	// Dado em tempo real — expira a cada 60 segundos.
	TTLBuses = 60 * time.Second

	// TTLGeocode define o tempo de vida de resultados de geocoding no cache.
	// Endereços raramente mudam — reavaliado a cada 30 dias.
	TTLGeocode = 30 * 24 * time.Hour
)
