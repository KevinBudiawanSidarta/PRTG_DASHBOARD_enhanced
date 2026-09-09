-- Seed active financial profiles and map all down sensors to services
DO $$
DECLARE
    org_id UUID := '11111111-1111-1111-1111-111111111111';
    erp_id UUID := '44444444-4444-4444-4444-000000000001';
    portal_id UUID := '44444444-4444-4444-4444-000000000002';
    wh_id UUID := '44444444-4444-4444-4444-000000000003';
    tx_id UUID := '44444444-4444-4444-4444-444444444444';
    office_id UUID := '44444444-4444-4444-4444-000000000005';
BEGIN
    -- Ensure financial profiles for all services
    -- Expire previous valid_to is null
    UPDATE financial_profiles SET valid_to = now() WHERE organization_id = org_id AND valid_to IS NULL;

    -- ERP Production Financial Profile
    INSERT INTO financial_profiles (
        organization_id, business_service_id, hourly_revenue, transactions_per_hour,
        avg_transaction_value, service_dependency, loss_probability, operational_cost_per_hour,
        penalty_config, recovery_config, valid_from, affected_employees, avg_employee_cost_per_hour
    ) VALUES (
        org_id, erp_id, 50000000, 200, 250000, 0.90, 0.80, 3000000,
        '{"fixed": 10000000}'::jsonb, '{"fixed": 5000000}'::jsonb, now(), 300, 75000
    );

    -- Customer Portal Financial Profile
    INSERT INTO financial_profiles (
        organization_id, business_service_id, hourly_revenue, transactions_per_hour,
        avg_transaction_value, service_dependency, loss_probability, operational_cost_per_hour,
        penalty_config, recovery_config, valid_from, affected_employees, avg_employee_cost_per_hour
    ) VALUES (
        org_id, portal_id, 90000000, 800, 112500, 0.95, 0.85, 5000000,
        '{"fixed": 25000000}'::jsonb, '{"fixed": 10000000}'::jsonb, now(), 50, 100000
    );

    -- Customer Transaction API Financial Profile
    INSERT INTO financial_profiles (
        organization_id, business_service_id, hourly_revenue, transactions_per_hour,
        avg_transaction_value, service_dependency, loss_probability, operational_cost_per_hour,
        penalty_config, recovery_config, valid_from, affected_employees, avg_employee_cost_per_hour
    ) VALUES (
        org_id, tx_id, 150000000, 1500, 100000, 0.98, 0.90, 8000000,
        '{"fixed": 50000000}'::jsonb, '{"fixed": 20000000}'::jsonb, now(), 40, 120000
    );

    -- Warehouse System Financial Profile
    INSERT INTO financial_profiles (
        organization_id, business_service_id, hourly_revenue, transactions_per_hour,
        avg_transaction_value, service_dependency, loss_probability, operational_cost_per_hour,
        penalty_config, recovery_config, valid_from, affected_employees, avg_employee_cost_per_hour
    ) VALUES (
        org_id, wh_id, 20000000, 150, 133333, 0.80, 0.70, 2000000,
        '{"fixed": 5000000}'::jsonb, '{"fixed": 2000000}'::jsonb, now(), 120, 50000
    );

    -- Office Network Financial Profile
    INSERT INTO financial_profiles (
        organization_id, business_service_id, hourly_revenue, transactions_per_hour,
        avg_transaction_value, service_dependency, loss_probability, operational_cost_per_hour,
        penalty_config, recovery_config, valid_from, affected_employees, avg_employee_cost_per_hour
    ) VALUES (
        org_id, office_id, 8000000, 50, 160000, 0.60, 0.50, 1000000,
        '{"fixed": 0}'::jsonb, '{"fixed": 1000000}'::jsonb, now(), 280, 60000
    );

    -- Also map all SRV-DNS down sensors to ERP Production
    INSERT INTO service_sensor_mapping (organization_id, business_service_id, prtg_sensor_id, dependency_weight)
    SELECT org_id, erp_id, s.id, 0.95
    FROM prtg_sensors s
    WHERE s.device_name = 'SRV-DNS'
    ON CONFLICT (business_service_id, prtg_sensor_id) DO NOTHING;

    -- Map Device (Ping, SNMP CPU Load) to Warehouse System
    INSERT INTO service_sensor_mapping (organization_id, business_service_id, prtg_sensor_id, dependency_weight)
    SELECT org_id, wh_id, s.id, 0.85
    FROM prtg_sensors s
    WHERE s.device_name = 'Device'
    ON CONFLICT (business_service_id, prtg_sensor_id) DO NOTHING;

    -- Map Router Indihome to Customer Portal
    INSERT INTO service_sensor_mapping (organization_id, business_service_id, prtg_sensor_id, dependency_weight)
    SELECT org_id, portal_id, s.id, 0.90
    FROM prtg_sensors s
    WHERE s.device_name = 'Router Indihome'
    ON CONFLICT (business_service_id, prtg_sensor_id) DO NOTHING;

    -- Map Mikrotik 192.168.10.1 SNMP Custom to Office Network
    INSERT INTO service_sensor_mapping (organization_id, business_service_id, prtg_sensor_id, dependency_weight)
    SELECT org_id, office_id, s.id, 0.80
    FROM prtg_sensors s
    WHERE s.device_name = 'Mikrotik 192.168.10.1'
    ON CONFLICT (business_service_id, prtg_sensor_id) DO NOTHING;

END $$;
