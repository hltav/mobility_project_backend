<?php

// Standalone preflight: no Laravel bootstrap, dotenv, migrations or writes.
try {
    $database = getenv('PGSQL_DATABASE');
    if (! in_array($database, ['open_transit_test', 'open_transit_schema_test'], true)) {
        throw new RuntimeException('Forbidden database');
    }
    $pdo = new PDO(
        'pgsql:host=postgres;port=5432;dbname='.$database.';sslmode=disable;connect_timeout=5',
        getenv('PGSQL_USERNAME'), getenv('PGSQL_PASSWORD'),
        [PDO::ATTR_ERRMODE => PDO::ERRMODE_EXCEPTION]
    );
    $pdo->exec('BEGIN READ ONLY');
    $pdo->exec("SET LOCAL statement_timeout = '5s'");
    $rows = $pdo->query("SELECT current_database() AS db, session_user AS login, current_user AS effective,
        r.rolsuper OR r.rolcreatedb OR r.rolcreaterole OR r.rolbypassrls
        OR EXISTS (SELECT 1 FROM pg_roles a WHERE (a.rolsuper OR a.rolcreatedb OR a.rolcreaterole OR a.rolbypassrls)
            AND pg_has_role(session_user, a.oid, 'MEMBER')) AS administrative,
        m.cluster_id, m.purpose,
        has_table_privilege(current_user, 'test_control.cluster_identity', 'INSERT,UPDATE,DELETE,TRUNCATE') AS marker_writable
        FROM pg_roles r CROSS JOIN test_control.cluster_identity m
        WHERE r.rolname = session_user AND m.singleton = true
        AND NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = 'open_transit')")->fetchAll(PDO::FETCH_ASSOC);
    $pdo->rollBack();
    if (count($rows) !== 1) {
        throw new RuntimeException('Missing unique marker');
    }
    $r = $rows[0];
    if ($r['db'] !== $database || $r['login'] !== getenv('PGSQL_USERNAME')
        || $r['effective'] !== $r['login'] || $r['purpose'] !== 'open-transit-tests'
        || ! getenv('TEST_PG_CLUSTER_ID') || $r['cluster_id'] !== getenv('TEST_PG_CLUSTER_ID')) {
        throw new RuntimeException('Identity mismatch');
    }
    if (getenv('TEST_PG_MODE') !== 'provision'
        && (! in_array($r['administrative'], [false, 0, '0', 'f', 'false'], true)
            || ! in_array($r['marker_writable'], [false, 0, '0', 'f', 'false'], true))) {
        throw new RuntimeException('Excessive privileges');
    }
} catch (Throwable $e) {
    // Do not disclose connection details, role names or driver error strings.
    fwrite(STDERR, "Test cluster verification failed; command blocked.\n");
    exit(1);
}
