<?php

namespace App\DTOs;

class BusDTO
{
    public function __construct(
        public readonly string $prefix,
        public readonly bool   $accessible,
        public readonly float  $latitude,
        public readonly float  $longitude,
    ) {}

    /**
     * Cria um DTO a partir do array mapeado pelo ExternalApiService.
     */
    public static function fromArray(array $data): self
    {
        return new self(
            prefix:     $data['prefix']     ?? '',
            accessible: $data['accessible'] ?? false,
            latitude:   $data['latitude']   ?? 0.0,
            longitude:  $data['longitude']  ?? 0.0,
        );
    }

    public function toArray(): array
    {
        return [
            'prefix'     => $this->prefix,
            'accessible' => $this->accessible,
            'latitude'   => $this->latitude,
            'longitude'  => $this->longitude,
        ];
    }
}
