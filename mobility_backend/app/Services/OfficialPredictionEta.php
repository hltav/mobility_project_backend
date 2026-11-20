<?php

namespace App\Services;

use Carbon\CarbonImmutable;

// Temporal interpretation only; stop identities are supplied by confirmed references.
class OfficialPredictionEta
{
    public static function unavailable(string $reason): array
    {
        return ['status'=>'unavailable','source'=>'sptrans','reason'=>$reason,
            'reference_stop_code'=>null,'predicted_arrival_at'=>null,'eta_seconds'=>null,
            'eta_minutes'=>null,'observed_at'=>null];
    }

    public function forStop(?array $s, int $cl, int $direction, string $prefix, int $cp, CarbonImmutable $now): array
    {
        try {
            if (!$s || ($s['schema_version']??null)!==1 || ($s['source']??null)!=='sptrans'
                || ($s['sptrans_line_id']??null)!==$cl || ($s['sptrans_direction']??null)!==$direction
                || ($s['status']??null)!=='available' || !empty($s['provider_error_code'])) {
                return self::unavailable('prediction_snapshot_or_identity_unavailable');
            }
            $fetched=$this->stamp($s['fetched_at']??null);
            $until=$this->stamp($s['valid_until']??null);
            if ($fetched->greaterThan($now) || !$until->greaterThan($fetched)
                || !$now->lessThan($until) || $now->getTimestamp()-$fetched->getTimestamp()>=20) {
                return self::unavailable('prediction_expired_or_invalid_time');
            }
            $matches=[];
            foreach ($s['predictions']['ps']??[] as $stop) {
                if (($stop['cp']??null)!==$cp) { continue; }
                foreach ($stop['vs']??[] as $vehicle) {
                    if (($vehicle['p']??null)===$prefix) { $matches[]=$vehicle; }
                }
            }
            if (count($matches)!==1) { return self::unavailable('prediction_vehicle_stop_missing_or_ambiguous'); }
            $v=$matches[0]; $capture=$this->stamp($v['ta']??null);
            if ($capture->greaterThan($fetched->addSecond()) || $fetched->getTimestamp()-$capture->getTimestamp()>120) {
                return self::unavailable('provider_observation_stale_or_invalid');
            }
            $hr=$this->clock($s['predictions']['hr']??null); $arrival=$this->clock($v['t']??null);
            $local=$fetched->setTimezone('America/Sao_Paulo'); $references=[];
            foreach ([-1,0,1] as $day) {
                $ref=$local->addDays($day)->setTime(...$hr);
                if (abs($ref->getTimestamp()-$fetched->getTimestamp())<=90
                    && abs($ref->getTimestamp()-$capture->getTimestamp())<=120) { $references[]=$ref; }
            }
            if (count($references)!==1) { return self::unavailable('provider_reference_date_unproved'); }
            $ref=$references[0]; $predicted=$ref->setTime(...$arrival);
            if ($predicted->lessThan($ref)) { $predicted=$predicted->addDay(); }
            $horizon=$predicted->getTimestamp()-$ref->getTimestamp();
            $seconds=$predicted->getTimestamp()-$now->getTimestamp();
            if ($horizon>7200 || $seconds<0) { return self::unavailable('prediction_past_or_outside_supported_horizon'); }
            return ['status'=>'available','source'=>'sptrans','reason'=>null,'reference_stop_code'=>$cp,
                'predicted_arrival_at'=>$predicted->toIso8601String(),'eta_seconds'=>$seconds,
                'eta_minutes'=>(int) ceil($seconds/60),'observed_at'=>$capture->toIso8601String()];
        } catch (\Throwable $e) { return self::unavailable('prediction_invalid_or_time_unproved'); }
    }

    private function stamp(mixed $value): CarbonImmutable
    {
        if (!is_string($value) || !preg_match('/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?(?:Z|[+-]\d\d:\d\d)$/',$value)) { throw new \InvalidArgumentException; }
        $date=CarbonImmutable::parse($value);
        if ($date->format('Y-m-d\TH:i:s')!==substr($value,0,19)) { throw new \InvalidArgumentException; }
        return $date;
    }

    private function clock(mixed $value): array
    {
        if (!is_string($value) || !preg_match('/^([01]\d|2[0-3]):([0-5]\d)(?::([0-5]\d))?$/',$value,$m)) { throw new \InvalidArgumentException; }
        return [(int)$m[1],(int)$m[2],(int)($m[3]??0)];
    }
}
