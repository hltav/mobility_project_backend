<?php

namespace App\Models;

use Illuminate\Database\Eloquent\Model;
use Illuminate\Database\Eloquent\Relations\BelongsTo;
use Illuminate\Database\Eloquent\Relations\HasMany;

class Trip extends Model
{
    protected $fillable = [
        'route_id',
        'gtfs_trip_id',
        'service_id',
        'headsign',
        'direction_id',
        'shape_id',
    ];

    public function route(): BelongsTo
    {
        return $this->belongsTo(Route::class);
    }

    public function tripStops(): HasMany
    {
        return $this->hasMany(TripStop::class);
    }
}
