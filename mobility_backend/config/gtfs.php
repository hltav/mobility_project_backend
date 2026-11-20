<?php

return [
    // Preview also requires APP_ENV=local/testing and authenticated access.
    'preview_enabled' => env('GTFS_PREVIEW_ENABLED', false),
    'vehicle_max_age_seconds' => 120,
    'vehicle_max_lateral_m' => 100,
];
