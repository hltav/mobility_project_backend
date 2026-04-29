<?php

namespace App\DTOs;

use App\Models\Line;

class LineDTO
{
    public function __construct(
        public readonly int    $id,
        public readonly string $code,
        public readonly string $name,
        public readonly string $origin,
        public readonly string $destination,
        public readonly bool   $circular,
        public readonly int    $sptransId,
    ) {}

    /**
     * Cria um DTO a partir do model Eloquent.
     */
    public static function fromModel(Line $line): self
    {
        return new self(
            id:          $line->id,
            code:        $line->code,
            name:        $line->name,
            origin:      $line->origin,
            destination: $line->destination,
            circular:    $line->circular,
            sptransId:   $line->sptrans_id,
        );
    }

    /**
     * Cria um DTO a partir do payload bruto da SPTrans.
     *
     * Campos SPTrans:
     *  cl  → código da linha (int)
     *  lt  → letreiro (ex: "8000")
     *  tp  → terminus principal
     *  ts  → terminus secundário
     *  lc  → circular (bool)
     */
    public static function fromSptrans(array $data): self
    {
        return new self(
            id:          0,
            code:        $data['lt'] ?? '',
            name:        ($data['tp'] ?? '') . ' - ' . ($data['ts'] ?? ''),
            origin:      $data['tp'] ?? '',
            destination: $data['ts'] ?? '',
            circular:    (bool) ($data['lc'] ?? false),
            sptransId:   $data['cl'] ?? 0,
        );
    }

    public function toArray(): array
    {
        return [
            'id'          => $this->id,
            'code'        => $this->code,
            'name'        => $this->name,
            'origin'      => $this->origin,
            'destination' => $this->destination,
            'circular'    => $this->circular,
            'sptrans_id'  => $this->sptransId,
        ];
    }
}
