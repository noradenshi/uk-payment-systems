TRUNCATE participant_liquidity, participant_statuses, participant_profiles, fps_dns_cycles CASCADE;

WITH seed_data (bic, name, sort_code, api_key, bal) AS (
    VALUES
        ('BARCGB2L', 'Barclays Bank', '20-00-00', 'ak_barcgb2l_dev', 500000.00),
        ('HSBCGB44', 'HSBC UK', '40-00-00', 'ak_hsbcgb44_dev', 300000.00),
        ('LLOYGB21', 'Lloyds Bank', '30-00-00', 'ak_lloygb21_dev', 400000.00),
        ('SNDRUK22', 'Alice Bank', '60-00-00', 'ak_sndruk22_dev', 500000.00)
),
ins_profiles AS (
    INSERT INTO participant_profiles (bic_code, name, sort_code, api_key, participant_type)
    SELECT bic, name, sort_code, api_key, 'DIRECT' FROM seed_data
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
