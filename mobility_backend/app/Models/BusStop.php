<?php

namespace App\Models;

use Illuminate\Database\Eloquent\Builder;
use Illuminate\Database\Eloquent\Model;
use Illuminate\Support\Facades\DB;
use Illuminate\Database\Eloquent\Relations\HasMany;

class BusStop extends Model
{
    protected $hidden = ['position_geom', 'position_geog', 'legacy_visible', 'attributes_provenance'];

    protected $fillable = [
        'gtfs_stop_id',
        'name',
        'description',
        'latitude',
        'longitude',
    ];

    protected $casts = [
        'latitude' => 'float',
        'longitude' => 'float',
    ];

    protected static function booted(): void
    {
        static::addGlobalScope('published_registry', function (Builder $query): void {
            if ($query->getModel()->getConnection()->getDriverName() === 'pgsql') {
                $query->where($query->getModel()->qualifyColumn('legacy_visible'), true);
            }
        });
    }

    public function getSptransCodeAttribute(mixed $value): mixed
    {
        if ($this->getConnection()->getDriverName() !== 'pgsql') {
            return $value;
        }

        $code = DB::connection($this->getConnectionName())->table('public.stop_source_references')
            ->where('stop_id', $this->getKey())->where('source_kind', 'sptrans')
            ->where('status', 'confirmed')->value('sptrans_stop_code');

        return $code === null ? null : (int) $code;
    }

    public function tripStops(): HasMany
    {
        return $this->hasMany(TripStop::class);
    }
}
