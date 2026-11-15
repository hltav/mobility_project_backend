<?php

namespace App\Models;

use Illuminate\Database\Eloquent\Model;
use Illuminate\Database\Eloquent\Relations\BelongsTo;

class RouteSptransMapping extends Model
{
    protected $fillable = [
        'route_id',
        'sptrans_line_id',
        'confidence',
        'matching_method',
    ];

    public function route(): BelongsTo
    {
        return $this->belongsTo(Route::class);
    }

    public function sptransLine(): BelongsTo
    {
        return $this->belongsTo(SptransLine::class);
    }
}
