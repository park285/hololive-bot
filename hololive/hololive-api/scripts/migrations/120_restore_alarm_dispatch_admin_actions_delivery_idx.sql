-- FK child(delivery_id) 참조 검사용 복구. idx_scan=0만으로는 관측 창에
-- parent delete/RI workload가 없었는지 배제할 수 없고, ON DELETE SET NULL은
-- child lookup을 요구한다 (db-audit B1).
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_alarm_dispatch_admin_actions_delivery
    ON alarm_dispatch_admin_actions (delivery_id);
