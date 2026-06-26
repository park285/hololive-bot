-- 선행 조건: 063_add_member_celebration_dates.sql 적용 필요
-- birthday 연도 = debut_date 연도 (캐릭터 생일은 월/일만 의미, 쿼리는 EXTRACT(MONTH/DAY)만 사용)
-- 데이터 출처: hololive.hololivepro.com/en/talents/

-- ============================================================
-- 홀로라이브 JP 0기생
-- ============================================================
UPDATE members SET birthday = '2017-05-15', debut_date = '2017-09-07' WHERE channel_id = 'UCp6993wxpyDPHUpavwDFqgg'; -- Tokino Sora
UPDATE members SET birthday = '2018-05-23', debut_date = '2018-03-09' WHERE channel_id = 'UCDqI2jOz0weumE8s7paEk6g'; -- Robocosan
UPDATE members SET birthday = '2018-03-05', debut_date = '2018-08-01' WHERE channel_id = 'UC-hM6YJuNYVAmUWxeIr9FeA'; -- Sakura Miko
UPDATE members SET birthday = '2018-07-01', debut_date = '2018-11-15' WHERE channel_id = 'UC0TXe_LYZ4scaW2XMyi5_kw'; -- AZKi
UPDATE members SET birthday = '2018-03-22', debut_date = '2018-03-22' WHERE channel_id = 'UC5CwaMl1eIgY8h02uZw7u8A'; -- Hoshimachi Suisei

-- ============================================================
-- 홀로라이브 JP 1기생
-- ============================================================
UPDATE members SET birthday = '2018-07-22', debut_date = '2018-06-01' WHERE channel_id = 'UCQ0UDLQCjY0rmuxCDE38FGg'; -- Natsuiro Matsuri
UPDATE members SET birthday = '2018-02-17', debut_date = '2018-06-01' WHERE channel_id = 'UCFTLzh12_nrtzqBPsTCqenA'; -- Aki Rosenthal
UPDATE members SET birthday = '2018-08-10', debut_date = '2018-06-02' WHERE channel_id = 'UC1CfXB_kRs3C-zaeTG3oGyg'; -- Akai Haato

-- ============================================================
-- 홀로라이브 JP 2기생
-- ============================================================
UPDATE members SET birthday = '2018-12-13', debut_date = '2018-09-03' WHERE channel_id = 'UC7fk0CB07ly8oSl0aqKkqFg'; -- Nakiri Ayame
UPDATE members SET birthday = '2018-02-14', debut_date = '2018-09-04' WHERE channel_id = 'UC1suqwovbL1kzsoaZgFZLKg'; -- Yuzuki Choco
UPDATE members SET birthday = '2018-07-02', debut_date = '2018-09-16' WHERE channel_id = 'UCvzGlP9oQwU--Y0r9id_jnA'; -- Oozora Subaru

-- ============================================================
-- 홀로라이브 GAMERS
-- ============================================================
UPDATE members SET birthday = '2018-10-05', debut_date = '2018-06-01' WHERE channel_id = 'UCdn5BQ06XqgXoAxIhbqw5Rg'; -- Shirakami Fubuki
UPDATE members SET birthday = '2018-08-20', debut_date = '2018-12-07' WHERE channel_id = 'UCp-5t9SrOQwXMU7iIjQfARg'; -- Ookami Mio
UPDATE members SET birthday = '2019-02-22', debut_date = '2019-04-06' WHERE channel_id = 'UCvaTdHTWBGv3MKj3KVqJVCw'; -- Nekomata Okayu
UPDATE members SET birthday = '2019-10-01', debut_date = '2019-04-13' WHERE channel_id = 'UChAnqc_AY5_I3Px5dig3X1Q'; -- Inugami Korone

-- ============================================================
-- 홀로라이브 JP 3기생
-- ============================================================
UPDATE members SET birthday = '2019-01-12', debut_date = '2019-07-17' WHERE channel_id = 'UC1DCedRgGHBdm81E1llLhOQ'; -- Usada Pekora
UPDATE members SET birthday = '2019-04-02', debut_date = '2019-08-07' WHERE channel_id = 'UCvInZx9h3jC2JzsIzoOebWg'; -- Shiranui Flare
UPDATE members SET birthday = '2019-11-24', debut_date = '2019-08-08' WHERE channel_id = 'UCdyqAaZDKHXg4Ahi7VENThQ'; -- Shirogane Noel
UPDATE members SET birthday = '2019-07-30', debut_date = '2019-08-11' WHERE channel_id = 'UCCzUftO8KOVkV4wQG1vkUvg'; -- Houshou Marine

-- ============================================================
-- 홀로라이브 JP 4기생
-- ============================================================
UPDATE members SET birthday = '2019-04-22', debut_date = '2019-12-27' WHERE channel_id = 'UCZlDXzGoo7d44bwdNObFacg'; -- Amane Kanata
UPDATE members SET birthday = '2019-06-06', debut_date = '2019-12-29' WHERE channel_id = 'UCqm3BQLlJfvkTsX_hvm0UmA'; -- Tsunomaki Watame
UPDATE members SET birthday = '2020-08-08', debut_date = '2020-01-03' WHERE channel_id = 'UC1uv2Oq6kNxgATlCiez59hw'; -- Tokoyami Towa
UPDATE members SET birthday = '2020-10-10', debut_date = '2020-01-04' WHERE channel_id = 'UCa9Y57gfeY0Zro_noHRVrnw'; -- Himemori Luna

-- ============================================================
-- 홀로라이브 JP 5기생
-- ============================================================
UPDATE members SET birthday = '2020-11-15', debut_date = '2020-08-12' WHERE channel_id = 'UCFKOVgVbGmX65RxO3EtH3iw'; -- Yukihana Lamy
UPDATE members SET birthday = '2020-03-02', debut_date = '2020-08-13' WHERE channel_id = 'UCAWSyEs_Io8MtpY3m-zqILA'; -- Momosuzu Nene
UPDATE members SET birthday = '2020-09-08', debut_date = '2020-08-14' WHERE channel_id = 'UCUKD-uaobj9jiqB-VXt71mA'; -- Shishiro Botan
UPDATE members SET birthday = '2020-01-30', debut_date = '2020-08-16' WHERE channel_id = 'UCK9V2B22uJYu3N7eR_BT9QA'; -- Omaru Polka

-- ============================================================
-- 홀로라이브 JP holoX (6기생)
-- ============================================================
UPDATE members SET birthday = '2021-05-25', debut_date = '2021-11-26' WHERE channel_id = 'UCENwRMx5Yh42zWpzURebzTw'; -- La+ Darknesss
UPDATE members SET birthday = '2021-06-11', debut_date = '2021-11-27' WHERE channel_id = 'UCs9_O1tRPMQTHQ-N_L6FU2g'; -- Takane Lui
UPDATE members SET birthday = '2021-03-15', debut_date = '2021-11-28' WHERE channel_id = 'UC6eWCld0KwmyHFbAqK3V-Rw'; -- Hakui Koyori
UPDATE members SET birthday = '2021-06-18', debut_date = '2021-11-30' WHERE channel_id = 'UC_vMYWcDjmfdpH6r4TTn1MQ'; -- Kazama Iroha

-- ============================================================
-- 홀로라이브 JP DEV_IS ReGLOSS
-- ============================================================
UPDATE members SET birthday = '2023-04-20', debut_date = '2023-09-09' WHERE channel_id = 'UCWQtYtq9EOB4-I5P-3fh8lA'; -- Otonose Kanade
UPDATE members SET birthday = '2023-05-12', debut_date = '2023-09-09' WHERE channel_id = 'UCtyWhCj3AqKh2dXctLkDtng'; -- Ichijou Ririka
UPDATE members SET birthday = '2023-02-04', debut_date = '2023-09-10' WHERE channel_id = 'UCdXAk5MpyLD8594lm_OvtGQ'; -- Juufuutei Raden
UPDATE members SET birthday = '2023-06-07', debut_date = '2023-09-10' WHERE channel_id = 'UC1iA6_NT4mtAcIII6ygrvCw'; -- Todoroki Hajime

-- ============================================================
-- 홀로라이브 JP DEV_IS FLOW GLOW
-- ============================================================
UPDATE members SET birthday = '2024-05-29', debut_date = '2024-11-09' WHERE channel_id = 'UC9LSiN9hXI55svYEBrrK-tw'; -- Isaki Riona
UPDATE members SET birthday = '2024-08-27', debut_date = '2024-11-09' WHERE channel_id = 'UCGzTVXqMQHa4AgJVJIVvtDQ'; -- Kikirara Vivi
UPDATE members SET birthday = '2024-07-25', debut_date = '2024-11-09' WHERE channel_id = 'UCuI_opAVX6qbxZY-a-AxFuQ'; -- Koganei Niko
UPDATE members SET birthday = '2024-06-16', debut_date = '2024-11-09' WHERE channel_id = 'UCjk2nKmHzgH5Xy-C5qYRd5A'; -- Mizumiya Su
UPDATE members SET birthday = '2024-07-08', debut_date = '2024-11-09' WHERE channel_id = 'UCKMWFR6lAstLa7Vbf5dH7ig'; -- Rindo Chihaya

-- ============================================================
-- 홀로라이브 ID 1기생
-- ============================================================
UPDATE members SET birthday = '2020-01-15', debut_date = '2020-04-10' WHERE channel_id = 'UCOyYb1c43VlX9rc_lT6NKQw'; -- Ayunda Risu
UPDATE members SET birthday = '2020-02-15', debut_date = '2020-04-11' WHERE channel_id = 'UCP0BspO_AMEe3aQqqpo89Dg'; -- Moona Hoshinova
UPDATE members SET birthday = '2020-07-15', debut_date = '2020-04-12' WHERE channel_id = 'UCAoy6rzhSf4ydcYjJw3WoVg'; -- Airani Iofifteen

-- ============================================================
-- 홀로라이브 ID 2기생
-- ============================================================
UPDATE members SET birthday = '2020-10-13', debut_date = '2020-12-04' WHERE channel_id = 'UCYz_5n-uDuChHtLo7My1HnQ'; -- Kureiji Ollie
UPDATE members SET birthday = '2020-03-12', debut_date = '2020-12-05' WHERE channel_id = 'UC727SQYUvx5pDDGQpTICNWg'; -- Anya Melfissa
UPDATE members SET birthday = '2020-09-09', debut_date = '2020-12-06' WHERE channel_id = 'UChgTyjG-pdNvxxhdsXfHQ5Q'; -- Pavolia Reine

-- ============================================================
-- 홀로라이브 ID 3기생
-- ============================================================
UPDATE members SET birthday = '2022-11-07', debut_date = '2022-03-25' WHERE channel_id = 'UCTvHWSfBZgtxE4sILOaurIQ'; -- Vestia Zeta
UPDATE members SET birthday = '2022-08-30', debut_date = '2022-03-26' WHERE channel_id = 'UCZLZ8Jjx_RN2CXloOmgTHVg'; -- Kaela Kovalskia
UPDATE members SET birthday = '2022-12-12', debut_date = '2022-03-27' WHERE channel_id = 'UCjLEmnpCNeisMxy134KPwWw'; -- Kobo Kanaeru

-- ============================================================
-- 홀로라이브 EN Myth
-- ============================================================
UPDATE members SET birthday = '2020-04-04', debut_date = '2020-09-12' WHERE channel_id = 'UCL_qhgtOy0dy1Agp8vkySQg'; -- Mori Calliope
UPDATE members SET birthday = '2020-07-06', debut_date = '2020-09-12' WHERE channel_id = 'UCHsx4Hqa-1ORjQTh9TYDhww'; -- Takanashi Kiara
UPDATE members SET birthday = '2020-05-20', debut_date = '2020-09-13' WHERE channel_id = 'UCMwGHR0BTZuLsmjY_NT5Pwg'; -- Ninomae Ina'nis

-- ============================================================
-- 홀로라이브 EN Project: HOPE
-- ============================================================
UPDATE members SET birthday = '2021-03-07', debut_date = '2021-07-11' WHERE channel_id = 'UC8rcEBzJSleTkf_-agPM20g'; -- IRyS

-- ============================================================
-- 홀로라이브 EN Promise
-- ============================================================
UPDATE members SET birthday = '2021-03-14', debut_date = '2021-08-23' WHERE channel_id = 'UCmbs8T6MWqUHP1tIQvSgKrg'; -- Ouro Kronii
UPDATE members SET birthday = '2020-02-29', debut_date = '2021-08-23' WHERE channel_id = 'UCgmPnx-EEeOrZSg5Tiw7ZRQ'; -- Hakos Baelz (2/29 생일 — 윤년 2020 사용)

-- ============================================================
-- 홀로라이브 EN Advent
-- ============================================================
UPDATE members SET birthday = '2023-05-02', debut_date = '2023-07-30' WHERE channel_id = 'UCgnfPPb9JI3e9A4cXHnWbyg'; -- Shiori Novella
UPDATE members SET birthday = '2023-04-14', debut_date = '2023-07-30' WHERE channel_id = 'UC9p_lqQ0FEDz327Vgf5JwqA'; -- Koseki Bijou
UPDATE members SET birthday = '2023-02-01', debut_date = '2023-07-31' WHERE channel_id = 'UCt9H_RpQzhxzlyBxFqrdHqA' AND english_name = 'Fuwawa Abyssgard'; -- Fuwawa Abyssgard
UPDATE members SET birthday = '2023-08-01', debut_date = '2023-07-31' WHERE channel_id = 'UCt9H_RpQzhxzlyBxFqrdHqA' AND english_name = 'Mococo Abyssgard'; -- Mococo Abyssgard
UPDATE members SET birthday = '2023-11-21', debut_date = '2023-07-31' WHERE channel_id = 'UC_sFNM0z0MWm9A6WlKPuMMg'; -- Nerissa Ravencroft

-- ============================================================
-- 홀로라이브 EN Justice
-- ============================================================
UPDATE members SET birthday = '2024-04-25', debut_date = '2024-06-22' WHERE channel_id = 'UCW5uhrG1eCBYditmhL0Ykjw'; -- Elizabeth Rose Bloodflame
UPDATE members SET birthday = '2024-10-18', debut_date = '2024-06-22' WHERE channel_id = 'UCDHABijvPBnJm7F-KlNME3w'; -- Gigi Murin
UPDATE members SET birthday = '2024-11-11', debut_date = '2024-06-23' WHERE channel_id = 'UCvN5h1ShZtc7nly3pezRayg'; -- Cecilia Immergreen
UPDATE members SET birthday = '2024-05-11', debut_date = '2024-06-23' WHERE channel_id = 'UCl69AEx4MdqMZH7Jtsm7Tig'; -- Raora Panthera
