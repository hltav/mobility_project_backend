<?php

namespace App\Models;

use Illuminate\Database\Eloquent\Model;
use Illuminate\Database\Eloquent\Relations\HasMany;

class Route extends Model
{
    protected $fillable = [
        'gtfs_route_id',
        'short_name',
        'long_name',
        'route_type',
        'color',
        'text_color',
    ];

    public function trips(): HasMany
    {
        return $this->hasMany(Trip::class);
    }

    public function mappings(): HasMany
    {
        return $this->hasMany(RouteSptransMapping::class);
    }
}
