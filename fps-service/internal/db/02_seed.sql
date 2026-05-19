TRUNCATE participant_liquidity, participant_statuses, participant_profiles, fps_dns_cycles CASCADE;

WITH seed_data (bic, name, bal) AS (
    VALUES
        ('BARCGB2L', 'Barclays Bank', 500000.00),
        ('HSBCGB44', 'HSBC UK', 300000.00),
        ('LLOYGB21', 'Lloyds Bank', 400000.00),
        ('SNDRUK22', 'Alice Bank', 500000.00)
),
ins_profiles AS (
    INSERT INTO participant_profiles (bic_code, name, participant_type)
    SELECT bic, name, 'DIRECT' FROM seed_data
    RETURNING bic_code
),
ins_statuses AS (
    INSERT INTO participant_statuses (bic_code, status)
    SELECT bic_code, 'ACTIVE' FROM ins_profiles
)
INSERT INTO participant_liquidity (bic_code, balance)
SELECT sd.bic, sd.bal
FROM seed_data sd;

INSERT INTO fps_dns_cycles (cycle_start, cycle_end, status)
VALUES (NOW() - INTERVAL '2 hours', NOW() + INTERVAL '2 hours', 'OPEN');
