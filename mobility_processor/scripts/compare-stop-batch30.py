#!/usr/bin/env python3
"""Manual-only wrapper around the existing matcher; no matching algorithm here."""
import argparse
import concurrent.futures
import csv
import json
from pathlib import Path
import subprocess
import time

parser = argparse.ArgumentParser()
parser.add_argument('--run-dry-runs', action='store_true', help='Explicitly invoke only the selected matcher dry-runs')
parser.add_argument('--after', required=True, type=Path)
args = parser.parse_args()
repo = Path(__file__).resolve().parents[2]
baseline = repo / 'docs/evidence/ope38-batch30-2026-11-20'
selection = json.loads((baseline / 'selection.json').read_text())
lines = selection['lines']
assert len(lines) == 60 and len({x['cl'] for x in lines}) == 60 and len({x['code'] for x in lines}) == 30
args.after.mkdir(parents=True, exist_ok=True)
if args.run_dry_runs:
    if any(args.after.glob('*.json')):
        raise SystemExit('Use a new output directory; never overwrite baseline/results.')
    deadline = time.monotonic() + 180

    def run(line):
        if time.monotonic() >= deadline:
            return
        command = ['docker', 'exec', 'mobility_processor', 'timeout', '12', '/tmp/ope38-matchroutes',
                   '--feed-version', '1', '--dry-run', '--line-id', str(line['id'])]
        try:
            result = subprocess.run(command, capture_output=True, text=True, timeout=15, check=False)
            (args.after / f"{line['id']}.err").write_text(result.stderr)
            if result.returncode == 0:
                (args.after / f"{line['id']}.json").write_text(result.stdout)
        except subprocess.TimeoutExpired:
            (args.after / f"{line['id']}.err").write_text('diagnostic timeout\n')

    with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
        list(pool.map(run, lines))

before = json.loads((baseline / 'batch-report.json').read_text())['reports']
def keyed(reports):
    return {(a['feed_version_id'], a['line_id'], a['pattern_id']): a
            for report in reports for a in report['assessments']}
old = keyed(before)
reports, missing = [], []
for line in lines:
    path = args.after / f"{line['id']}.json"
    if not path.exists():
        missing.append(line['id'])
        continue
    report = json.loads(path.read_text())
    assert report['mode'] == 'dry_run' and report['feed_version_id'] == 1 and report['line_id'] == line['id']
    actual = report['operational_lines'][0]
    assert actual['cl'] == line['cl'] and actual['code'] == line['code'] and actual['sptrans_direction'] == line['sptrans_direction']
    assert {peer['id'] for peer in lines if peer['code'] == line['code']}.issubset(report['comparison_line_ids'])
    reports.append(report)
new = keyed(reports)
summary = dict(completed_cls=len(reports), missing_cls=missing, assessments_before=len(old), assessments_after=len(new),
               orientation_improved=0, remained_insufficient=0, became_ambiguous=0, became_contradicted=0,
               coverage_improved=0, added_assessments=0, removed_assessments=0)
rows = []
completed_ids = {report['line_id'] for report in reports}
for key in sorted(set(old) | set(new)):
    if key[1] not in completed_ids:
        continue
    a, b = old.get(key), new.get(key)
    if not a: summary['added_assessments'] += 1
    if not b: summary['removed_assessments'] += 1
    if a and b:
        summary['orientation_improved'] += a['evidence_status'] != 'supported' and b['evidence_status'] == 'supported'
        summary['remained_insufficient'] += a['evidence_status'] == b['evidence_status'] == 'insufficient'
        summary['became_ambiguous'] += a['evidence_status'] != 'ambiguous' and b['evidence_status'] == 'ambiguous'
        summary['became_contradicted'] += a['evidence_status'] != 'contradicted' and b['evidence_status'] == 'contradicted'
        summary['coverage_improved'] += b['stop_evidence']['coverage'] > a['stop_evidence']['coverage']
    row = dict(feed_version_id=key[0], line_id=key[1], pattern_id=key[2], cl=(b or a)['sptrans_line_id'],
               sptrans_direction=(b or a)['sptrans_direction'], before=a['evidence_status'] if a else 'absent',
               after=b['evidence_status'] if b else 'absent', stops_before=a['stop_evidence']['available'] if a else None,
               stops_after=b['stop_evidence']['available'] if b else None, coverage_before=a['stop_evidence']['coverage'] if a else None,
               coverage_after=b['stop_evidence']['coverage'] if b else None)
    rows.append(row)
(args.after / 'comparison.json').write_text(json.dumps({'summary': summary, 'rows': rows}, ensure_ascii=False, indent=2) + '\n')
if rows:
    with (args.after / 'comparison.csv').open('w', newline='') as f:
        writer = csv.DictWriter(f, fieldnames=list(rows[0]))
        writer.writeheader()
        writer.writerows(rows)
print(json.dumps(summary, ensure_ascii=False, indent=2))
