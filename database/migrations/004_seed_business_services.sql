-- Seed business services and mappings for BIA
DO $$
DECLARE
    org_id UUID := '11111111-1111-1111-1111-111111111111';
    erp_id UUID := '44444444-4444-4444-4444-000000000001';
    portal_id UUID := '44444444-4444-4444-4444-000000000002';
    wh_id UUID := '44444444-4444-4444-4444-000000000003';
    tx_id UUID := '44444444-4444-4444-4444-444444444444';
    office_id UUID := '44444444-4444-4444-4444-000000000005';
BEGIN
    -- Update existing Transaction API
    UPDATE business_services SET
        owner_name = 'Payment & Core Banking',
        affected_users = 3200,
        sla_target_pct = 99.99,
        rto_minutes = 10,
        rpo_minutes = 5,
        description = 'Transaction routing, payment gateways, and checkout API processing',
        business_value_per_hour = 120000000,
        updated_at = now()
    WHERE id = tx_id;

    -- Insert ERP Production
    INSERT INTO business_services (id, organization_id, name, criticality, owner_name, affected_users, sla_target_pct, rto_minutes, rpo_minutes, description, business_value_per_hour)
    VALUES (erp_id, org_id, 'ERP Production', 'P1', 'IT Operations & Enterprise Apps', 450, 99.90, 30, 15, 'Critical ERP backend powering inventory, financial billing, and branch invoicing', 45000000)
    ON CONFLICT (id) DO UPDATE SET
        owner_name = EXCLUDED.owner_name,
        affected_users = EXCLUDED.affected_users,
        sla_target_pct = EXCLUDED.sla_target_pct,
        rto_minutes = EXCLUDED.rto_minutes,
        rpo_minutes = EXCLUDED.rpo_minutes,
        description = EXCLUDED.description,
        business_value_per_hour = EXCLUDED.business_value_per_hour;

    -- Insert Customer Portal
    INSERT INTO business_services (id, organization_id, name, criticality, owner_name, affected_users, sla_target_pct, rto_minutes, rpo_minutes, description, business_value_per_hour)
    VALUES (portal_id, org_id, 'Customer Portal', 'P1', 'Digital Channels', 1800, 99.95, 15, 5, 'External web and mobile customer facing portal for account access and ordering', 80000000)
    ON CONFLICT (id) DO UPDATE SET
        owner_name = EXCLUDED.owner_name,
        affected_users = EXCLUDED.affected_users,
        sla_target_pct = EXCLUDED.sla_target_pct,
        rto_minutes = EXCLUDED.rto_minutes,
        rpo_minutes = EXCLUDED.rpo_minutes,
        description = EXCLUDED.description,
        business_value_per_hour = EXCLUDED.business_value_per_hour;

    -- Insert Warehouse System
    INSERT INTO business_services (id, organization_id, name, criticality, owner_name, affected_users, sla_target_pct, rto_minutes, rpo_minutes, description, business_value_per_hour)
    VALUES (wh_id, org_id, 'Warehouse System', 'P2', 'Supply Chain & Logistics', 120, 99.50, 60, 30, 'Barcode scanning, order fulfillment, and logistics dispatch in warehouse', 18000000)
    ON CONFLICT (id) DO UPDATE SET
        owner_name = EXCLUDED.owner_name,
        affected_users = EXCLUDED.affected_users,
        sla_target_pct = EXCLUDED.sla_target_pct,
        rto_minutes = EXCLUDED.rto_minutes,
        rpo_minutes = EXCLUDED.rpo_minutes,
        description = EXCLUDED.description,
        business_value_per_hour = EXCLUDED.business_value_per_hour;

    -- Insert Office Network & Corporate Services
    INSERT INTO business_services (id, organization_id, name, criticality, owner_name, affected_users, sla_target_pct, rto_minutes, rpo_minutes, description, business_value_per_hour)
    VALUES (office_id, org_id, 'Office Network & Corporate Services', 'P3', 'IT Infrastructure', 280, 99.00, 120, 60, 'Internal office Wi-Fi, LAN switches, and corporate intranet', 6000000)
    ON CONFLICT (id) DO UPDATE SET
        owner_name = EXCLUDED.owner_name,
        affected_users = EXCLUDED.affected_users,
        sla_target_pct = EXCLUDED.sla_target_pct,
        rto_minutes = EXCLUDED.rto_minutes,
        rpo_minutes = EXCLUDED.rpo_minutes,
        description = EXCLUDED.description,
        business_value_per_hour = EXCLUDED.business_value_per_hour;

    -- Clear old mappings for these services (if re-running)
    DELETE FROM service_sensor_mapping WHERE organization_id = org_id AND business_service_id IN (erp_id, portal_id, wh_id, tx_id, office_id);

    -- Map ERP Production to SRV-DNS and Probe Device sensors
    INSERT INTO service_sensor_mapping (organization_id, business_service_id, prtg_sensor_id, dependency_weight)
    SELECT org_id, erp_id, id, 0.90 FROM prtg_sensors WHERE id IN ('e5316685-5a9b-4a0e-bf74-52571a30cd4b', '8ddd8139-5c2b-4362-aa60-2f316d86a1ef', '2d514a5b-fc55-4daa-9916-7eb125a0b3b1')
    ON CONFLICT DO NOTHING;

    -- Map Customer Portal to HTTP Google and Omada sensors
    INSERT INTO service_sensor_mapping (organization_id, business_service_id, prtg_sensor_id, dependency_weight)
    SELECT org_id, portal_id, id, 0.95 FROM prtg_sensors WHERE id IN ('8b13eb14-6b8a-4990-9513-8584688863ff', '4b914111-ebab-4519-a9ab-87a5fb5a91ef', '5b5dad33-f479-46e1-a5dd-a618b9f27a73')
    ON CONFLICT DO NOTHING;

    -- Map Warehouse System to NAS and local devices
    INSERT INTO service_sensor_mapping (organization_id, business_service_id, prtg_sensor_id, dependency_weight)
    SELECT org_id, wh_id, id, 0.80 FROM prtg_sensors WHERE id IN ('63d0baa8-7ac4-4fb5-b548-0ce4e31e6b72', 'c039c5bc-a765-491c-b150-db40d701e3d5', '4ee787e0-44cd-4e66-8ac3-be3aee6cfe70')
    ON CONFLICT DO NOTHING;

    -- Map Customer Transaction API
    INSERT INTO service_sensor_mapping (organization_id, business_service_id, prtg_sensor_id, dependency_weight)
    SELECT org_id, tx_id, id, 0.98 FROM prtg_sensors WHERE id IN ('4d01c372-930e-4ea3-95b3-a03c81fd7715', 'b1abf6b7-29fd-4c05-82ff-461a982eda9f', '82542ede-d171-4856-8f7b-1180a5de8ab7')
    ON CONFLICT DO NOTHING;

    -- Map Office Network & Corporate Services
    INSERT INTO service_sensor_mapping (organization_id, business_service_id, prtg_sensor_id, dependency_weight)
    SELECT org_id, office_id, id, 0.70 FROM prtg_sensors WHERE id IN ('9abe95f6-cc8f-4f14-bbe0-7dcf1dc0e8cc', 'bf22b318-b033-4513-b19a-3226755dcf43', 'd56d6847-d89f-4d02-9887-c2d0303dd18c')
    ON CONFLICT DO NOTHING;

END $$;
