<?php

namespace App\Models;

use Illuminate\Database\Eloquent\Model;
use Illuminate\Database\Eloquent\Relations\HasMany;

class SptransLine extends Model
{
    protected $table = 'sptrans_lines';

    protected $fillable = [
        'code',
        'name',
        'origin',
        'destination',
        'circular',
        'sptrans_id',
        'origin_lat',
        'origin_lng',
        'destination_lat',
        'destination_lng',
    ];

    public function buses(): HasMany
    {
        return $this->hasMany(Bus::class);
    }
}
