TRUNCATE participant_liquidity, participant_statuses, participant_profiles CASCADE;

WITH seed_data (bic, name, bal, su_code, is_su, is_dsu) AS (
    VALUES
        ('BARCGB2L', 'Barclays Bank', 1000000.00, 'SU-BARC', true, true),
        ('HSBCGB44', 'HSBC UK', 800000.00, 'SU-HSBC', true, true),
        ('LLOYGB21', 'Lloyds Bank', 750000.00, 'SU-LLOY', true, true),
        ('SNDRUK22', 'Alice Bank', 500000.00, 'SU-ALCE', true, true)
),
ins_profiles AS (
    INSERT INTO participant_profiles (bic_code, name, su_code, is_service_user, is_destination_user)
    SELECT bic, name, su_code, is_su::boolean, is_dsu::boolean FROM seed_data
    RETURNING bic_code
),
ins_statuses AS (
    INSERT INTO participant_statuses (bic_code, status)
    SELECT bic_code, 'ACTIVE' FROM ins_profiles
)
INSERT INTO participant_liquidity (bic_code, balance)
SELECT sd.bic, sd.bal FROM seed_data sd;

-- Seed a current open cycle
INSERT INTO bacs_cycles (input_date, processing_date, settlement_date, status)
VALUES (
    CURRENT_DATE,
    CURRENT_DATE + INTERVAL '1 day',
    CURRENT_DATE + INTERVAL '2 days',
    'OPEN'
);
