# YouTube Community Shorts Observation Data Collection Criteria

유튜브 커뮤니티/쇼츠 알람의 배포 후 24시간 관찰 구간에서 어떤 운영 채널을 포함하고, 어떤 시각대를 기준으로 비교하며, 게시물과 알람을 어떤 키로 대조할지 고정하는 기준 문서입니다.

## Scope

- 대상은 `COMMUNITY_POST`, `NEW_SHORT` 두 종류만 포함합니다.
- 목적은 데이터 수집 기준 확정입니다. 다른 알람 유형, fan-out 외 경로, UI는 이 문서 범위에 포함하지 않습니다.
- observation 기반 검증에서는 `recent_window` 결과와 섞지 않고 같은 `observation key`만 사용합니다.

## 1. Operational Channel Roster

24시간 관찰 구간의 운영 채널 목록은 아래 두 층으로 고정합니다.

| Layer | Canonical source | Inclusion rule | Exclusion rule | Canonical field |
| --- | --- | --- | --- | --- |
| 운영 채널 전체 roster | `postgres.members -> resolveCommunityShortsOperationalChannels` | `IsGraduated = false` 이고 `channel_id != ''` 인 멤버 | 졸업 멤버, 빈 `channel_id` | `channel_id` |
| 실제 알람 기대 route subset | `postgres.alarms alarm_types` 를 baseline에 합친 `channels[].routes[]` | 같은 `channel_id` 에서 `alarm_type in (COMMUNITY, SHORTS)` 이고 `alarm_enabled = true` | typed 알람 room 구독이 없는 route | `channel_id + alarm_type` |

규칙:

- 24시간 관찰 시작 시점에는 `go run ./hololive/hololive-youtube-producer/cmd/ops/youtube-community-shorts target-baseline` 를 실행해 `channels[]` snapshot을 남기고, 그 산출물을 해당 observation session의 roster 증적으로 사용합니다.
- `owner_label` 은 표시용이며 대조 키로 쓰지 않습니다. 채널 축의 canonical 값은 항상 `channel_id` 입니다.
- 운영 채널 전체 roster는 “관찰 대상 universe”를 뜻합니다. 실제 알람 누락/중복 대조는 그중 `routes[].alarm_enabled = true` 인 `channel_id + alarm_type` 조합에 한정합니다.
- route 상태 drift를 보기 위해 baseline을 다시 수집할 수는 있지만, 같은 24시간 관찰 구간의 최초 scope 판정은 시작 시점 snapshot을 기준으로 유지합니다.

### 1.1 Full Channel List Recorded In This Document

- 아래 표는 이 문서가 명시적으로 기록하는 unique `channel_id` 기준 운영 채널 universe 입니다. 리포지토리 안에서 재구성 가능한 roster source(`official_talents.json`, `official_profiles_raw.json`, manual member migrations)를 합쳐 `channel_id` canonical 목록으로 정리했습니다.
- channel count는 총 `76`개이며, `Hololive 62`, `Independents 2`, `Stellive 7`, `VSPO 5` 입니다.
- `status` 열은 설명용 메타데이터입니다. 실제 포함/제외 규칙은 여전히 `IsGraduated = false` 와 `channel_id != ''` 이고, exact-once/SLA 실제 대조 범위는 observation 시작 시점 baseline의 `routes[].alarm_enabled = true` 인 `channel_id + alarm_type` subset입니다.
- `Fuwawa Abyssgard` 와 `Mococo Abyssgard` 는 동일 YouTube `channel_id` 를 공유하므로 canonical roster row 1건으로 병합했습니다.
- 현재 repo-known official dataset에서 `channel_id` 가 비어 있는 `Otonose Kanade`, `Ichijou Ririka`, `Todoroki Hajime` 는 `channel_id != ''` 규칙 때문에 운영 채널 universe와 아래 표에서 제외합니다.


| Owner label | Org | Status | Slug | channel_id | Included scope |
| --- | --- | --- | --- | --- | --- |
| AZKi | Hololive | active | `azki` | `UC0TXe_LYZ4scaW2XMyi5_kw` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Airani Iofifteen | Hololive | active | `airani-iofifteen` | `UCAoy6rzhSf4ydcYjJw3WoVg` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Akai Haato | Hololive | active | `akai-haato` | `UC1CfXB_kRs3C-zaeTG3oGyg` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Aki Rosenthal | Hololive | active | `aki-rosenthal` | `UCFTLzh12_nrtzqBPsTCqenA` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Amane Kanata | Hololive | active | `amane-kanata` | `UCZlDXzGoo7d44bwdNObFacg` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Anya Melfissa | Hololive | active | `anya-melfissa` | `UC727SQYUvx5pDDGQpTICNWg` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Ayunda Risu | Hololive | active | `ayunda-risu` | `UCOyYb1c43VlX9rc_lT6NKQw` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Cecilia Immergreen | Hololive | active | `cecilia-immergreen` | `UCvN5h1ShZtc7nly3pezRayg` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Elizabeth Rose Bloodflame | Hololive | active | `elizabeth-rose-bloodflame` | `UCW5uhrG1eCBYditmhL0Ykjw` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Fuwawa Abyssgard / Mococo Abyssgard | Hololive | active | `fuwawa-abyssgard / mococo-abyssgard` | `UCt9H_RpQzhxzlyBxFqrdHqA` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Gigi Murin | Hololive | active | `gigi-murin` | `UCDHABijvPBnJm7F-KlNME3w` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Hakos Baelz | Hololive | active | `hakos-baelz` | `UCgmPnx-EEeOrZSg5Tiw7ZRQ` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Hakui Koyori | Hololive | active | `hakui-koyori` | `UC6eWCld0KwmyHFbAqK3V-Rw` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Himemori Luna | Hololive | active | `himemori-luna` | `UCa9Y57gfeY0Zro_noHRVrnw` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Hoshimachi Suisei | Hololive | active | `hoshimachi-suisei` | `UC5CwaMl1eIgY8h02uZw7u8A` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Houshou Marine | Hololive | active | `houshou-marine` | `UCCzUftO8KOVkV4wQG1vkUvg` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| IRyS | Hololive | active | `irys` | `UC8rcEBzJSleTkf_-agPM20g` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Inugami Korone | Hololive | active | `inugami-korone` | `UChAnqc_AY5_I3Px5dig3X1Q` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Isaki Riona | Hololive | active | `isaki-riona` | `UC9LSiN9hXI55svYEBrrK-tw` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Juufuutei Raden | Hololive | active | `juufuutei-raden` | `UCdXAk5MpyLD8594lm_OvtGQ` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Kaela Kovalskia | Hololive | active | `kaela-kovalskia` | `UCZLZ8Jjx_RN2CXloOmgTHVg` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Kazama Iroha | Hololive | active | `kazama-iroha` | `UC_vMYWcDjmfdpH6r4TTn1MQ` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Kikirara Vivi | Hololive | active | `kikirara-vivi` | `UCGzTVXqMQHa4AgJVJIVvtDQ` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Kobo Kanaeru | Hololive | active | `kobo-kanaeru` | `UCjLEmnpCNeisMxy134KPwWw` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Koganei Niko | Hololive | active | `koganei-niko` | `UCuI_opAVX6qbxZY-a-AxFuQ` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Koseki Bijou | Hololive | active | `koseki-bijou` | `UC9p_lqQ0FEDz327Vgf5JwqA` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Kureiji Ollie | Hololive | active | `kureiji-ollie` | `UCYz_5n-uDuChHtLo7My1HnQ` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| La+ Darknesss | Hololive | active | `la-darknesss` | `UCENwRMx5Yh42zWpzURebzTw` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Mizumiya Su | Hololive | active | `mizumiya-su` | `UCjk2nKmHzgH5Xy-C5qYRd5A` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Momosuzu Nene | Hololive | active | `momosuzu-nene` | `UCAWSyEs_Io8MtpY3m-zqILA` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Moona Hoshinova | Hololive | active | `moona-hoshinova` | `UCP0BspO_AMEe3aQqqpo89Dg` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Mori Calliope | Hololive | active | `mori-calliope` | `UCL_qhgtOy0dy1Agp8vkySQg` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Nakiri Ayame | Hololive | active | `nakiri-ayame` | `UC7fk0CB07ly8oSl0aqKkqFg` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Natsuiro Matsuri | Hololive | active | `natsuiro-matsuri` | `UCQ0UDLQCjY0rmuxCDE38FGg` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Nekomata Okayu | Hololive | active | `nekomata-okayu` | `UCvaTdHTWBGv3MKj3KVqJVCw` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Nerissa Ravencroft | Hololive | active | `nerissa-ravencroft` | `UC_sFNM0z0MWm9A6WlKPuMMg` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Ninomae Ina'nis | Hololive | active | `ninomae-inanis` | `UCMwGHR0BTZuLsmjY_NT5Pwg` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Omaru Polka | Hololive | active | `omaru-polka` | `UCK9V2B22uJYu3N7eR_BT9QA` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Ookami Mio | Hololive | active | `ookami-mio` | `UCp-5t9SrOQwXMU7iIjQfARg` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Oozora Subaru | Hololive | active | `oozora-subaru` | `UCvzGlP9oQwU--Y0r9id_jnA` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Ouro Kronii | Hololive | active | `ouro-kronii` | `UCmbs8T6MWqUHP1tIQvSgKrg` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Pavolia Reine | Hololive | active | `pavolia-reine` | `UChgTyjG-pdNvxxhdsXfHQ5Q` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Raora Panthera | Hololive | active | `raora-panthera` | `UCl69AEx4MdqMZH7Jtsm7Tig` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Rindo Chihaya | Hololive | active | `rindo-chihaya` | `UCKMWFR6lAstLa7Vbf5dH7ig` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Robocosan | Hololive | active | `roboco-san` | `UCDqI2jOz0weumE8s7paEk6g` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Sakamata Chloe | Hololive | ended | `sakamata-chloe` | `UCIBY1ollUsauvVi4hW4cumw` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Sakura Miko | Hololive | active | `sakuramiko` | `UC-hM6YJuNYVAmUWxeIr9FeA` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Shiori Novella | Hololive | active | `shiori-novella` | `UCgnfPPb9JI3e9A4cXHnWbyg` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Shirakami Fubuki | Hololive | active | `shirakami-fubuki` | `UCdn5BQ06XqgXoAxIhbqw5Rg` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Shiranui Flare | Hololive | active | `shiranui-flare` | `UCvInZx9h3jC2JzsIzoOebWg` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Shirogane Noel | Hololive | active | `shirogane-noel` | `UCdyqAaZDKHXg4Ahi7VENThQ` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Shishiro Botan | Hololive | active | `shishiro-botan` | `UCUKD-uaobj9jiqB-VXt71mA` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Takanashi Kiara | Hololive | active | `takanashi-kiara` | `UCHsx4Hqa-1ORjQTh9TYDhww` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Takane Lui | Hololive | active | `takane-lui` | `UCs9_O1tRPMQTHQ-N_L6FU2g` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Tokino Sora | Hololive | active | `tokino-sora` | `UCp6993wxpyDPHUpavwDFqgg` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Tokoyami Towa | Hololive | active | `tokoyami-towa` | `UC1uv2Oq6kNxgATlCiez59hw` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Tsunomaki Watame | Hololive | active | `tsunomaki-watame` | `UCqm3BQLlJfvkTsX_hvm0UmA` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Usada Pekora | Hololive | active | `usada-pekora` | `UC1DCedRgGHBdm81E1llLhOQ` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Vestia Zeta | Hololive | active | `vestia-zeta` | `UCTvHWSfBZgtxE4sILOaurIQ` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Watson Amelia | Hololive | ended | `watson-amelia` | `UCyl1z3jo3XHR1riLFKG5UAg` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Yukihana Lamy | Hololive | active | `yukihana-lamy` | `UCFKOVgVbGmX65RxO3EtH3iw` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Yuzuki Choco | Hololive | active | `yuzuki-choco` | `UC1suqwovbL1kzsoaZgFZLKg` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Sameko Saba | Independents | active | `sameko-saba` | `UCxsZ6NCzjU_t4YSxQLBcM5A` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Yuuki Sakuna | Independents | active | `yuuki-sakuna` | `UCrV1Hf5r8P148idjoSfrGEQ` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Akane Lize | Stellive | active | `akane-lize` | `UC9m5xP6u69zXpD7MscY-uYQ` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Arahashi Tabi | Stellive | active | `arahashi-tabi` | `UCq-U-D8O6_6e4X6r-z9V0w` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Ayatsuno Yuni | Stellive | active | `ayatsuno-yuni` | `UClbYIn9LDbbFZ9w2shX3K0g` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Hanako Nana | Stellive | active | `hanako-nana` | `UCcA21_PzN1EhNe7xS4MJGsQ` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Neneko Mashiro | Stellive | active | `neneko-mashiro` | `UC9o9D7U5O8V0A-zO0v7UeLw` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Shirayuki Hina | Stellive | active | `shirayuki-hina` | `UC99CUC6yR6O_uXyS_3K7yKA` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Tenko Shibuki | Stellive | active | `tenko-shibuki` | `UCYxLMfeX1CbMBll9MsGlzmw` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Ichinose Uruha | VSPO | active | `ichinose-uruha` | `UC5LyYg6cCA4yHEYvtUsir3g` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Kaga Nazuna | VSPO | active | `kaga-nazuna` | `UCiMG6VdScBabPhJ1ZtaVmbw` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Kaminari Qpi | VSPO | active | `kaminari-qpi` | `UCMp55EbT_ZlqiMS3lCj01BQ` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Tachibana Hinano | VSPO | active | `tachibana-hinano` | `UCvUc0m317LWTTPZoBQV479A` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |
| Yakumo Beni | VSPO | active | `yakumo-beni` | `UCjXBuHmWkieBApgBhDuJMMQ` | Operational roster; exact-once/SLA is baseline route-gated (`alarm_enabled=true`). |



## 2. Common Time Basis

모든 저장/조인/판정 기준 시각대는 `UTC canonical` 로 통일합니다.

| Field / concept | Canonical basis | Rule |
| --- | --- | --- |
| observation key | `runtime_name + bigbang_cutover_at` | CLI와 저장소 모두 RFC3339 UTC 기준으로 동일하게 기록합니다. |
| observation window | `[observation_started_at, observation_ended_at)` | 경계는 inclusive-exclusive 로 읽습니다. active 상태에서는 `[observation_started_at, min(now, observation_ended_at))` 를 사용합니다. |
| 게시물 창 포함 기준 | `COALESCE(actual_published_at, detected_at)` | `actual_published_at` 가 있으면 실제 유튜브 게시 시각을 우선 사용하고, 비어 있을 때만 `detected_at` 으로 대체합니다. |
| SLA 시작점 | `actual_published_at` | `actual_published_at` 가 없을 때만 fallback으로 `detected_at` 을 사용합니다. |
| 알람 완료 시각 | `alarm_sent_at` | canonical 최초 성공 발송 시각입니다. |

추가 규칙:

- 대시보드나 운영 메모에서 KST를 표시할 수는 있지만, 저장/비교/합격선 판정은 모두 UTC timestamp를 기준으로 수행합니다.
- KST 표기를 운영 기록에 남길 때도 동일 row의 UTC 원문(`RFC3339`)을 함께 남겨 observation key와 post key 조인을 깨지 않도록 합니다.
- `TZ=Asia/Seoul` 런타임 환경이 있어도 DB에서 읽은 관찰/비교 시각은 UTC로 정규화된 값만 사용합니다.

## 3. Post-To-Alarm Correlation Keys

게시물과 알람을 대조할 때는 아래 키 우선순위를 사용합니다.

| Layer | Canonical key | Primary source | Usage |
| --- | --- | --- | --- |
| Observation session | `observation_runtime_name + observation_bigbang_cutover_at` | `youtube_community_shorts_observation_windows` | 같은 24시간 관찰 세션의 모든 조회를 묶는 최상위 키 |
| Route expectation | `channel_id + alarm_type` | baseline `channels[].routes[]` | 어떤 채널/타입 조합에 알람이 기대되는지 판정 |
| Canonical post comparison key | `alarm_type + channel_id + post_id` | `send-counts`, `delivery-logs`, sent-history outputs | 운영자가 게시물 1건을 식별하는 기본 키 |
| Tracking/storage join key | `kind + canonical_content_id` | `youtube_content_alarm_tracking` | raw tracking row와 canonical 게시물 식별자를 연결할 때 사용 |
| Single-send state key | `(kind, post_id)` | `youtube_community_shorts_alarm_states` | 게시물당 정확히 1개의 알람 상태 레코드를 보장하는 키 |
| Observation frozen baseline key | `(runtime_name, bigbang_cutover_at, kind, post_id)` | `youtube_community_shorts_observation_post_baselines` | 닫힌 24시간 관찰 구간의 최종 기준 집합 |
| Fan-out root | `outbox_id` | `youtube_notification_outbox`, delivery telemetry | 게시물 1건의 room fan-out 묶음 |
| Room attempt | `delivery_id + attempt_ordinal` | `youtube_notification_delivery_telemetry` | 같은 room delivery의 재시도 체인 추적 |

`post_id` 정규화 규칙:

- community canonical ID는 `community:<normalized-post-id>` 형식입니다. raw 값이 URL이어도 `/post/<id>` tail을 추출해 같은 canonical ID로 정규화합니다.
- shorts canonical ID는 `short:<video-id>` 형식입니다.
- telemetry/report의 `post_id` 는 payload의 `canonical_post_id` 를 우선 사용하고, 비어 있으면 `content_id`, 그다음 raw resource ID로 대체합니다.
- `COMMUNITY_POST -> COMMUNITY`, `NEW_SHORT -> SHORTS` 로 변환한 뒤에만 운영 대조 키(`alarm_type + channel_id + post_id`)를 만듭니다.
- `dedupe_key = youtube-notification:<kind>:<content_id>` 는 dedupe root 진단용 보조 키입니다. 운영 합격선의 기본 비교 키로 쓰지 않습니다.

불일치 처리 규칙:

- direct canonical key가 맞지 않아도 `actual_published_at + title_hint` 가 강하게 일치하면 `identifier mismatch candidate` 로만 보조 묶음을 만듭니다.
- `identifier mismatch candidate` 는 manual review 대상일 뿐이며, exact-once 합격/실패 판정을 자동으로 닫는 primary key가 아닙니다.
- exact-once 최종 판정은 항상 canonical post comparison key 기준으로 수행하고, 같은 `post_id + room_id` success가 2건 이상이면 duplicate입니다.

## 4. Required Collection Package For One Observation Window

같은 24시간 관찰 구간의 비교 패키지는 아래 순서로 수집합니다.

1. 관찰 시작 직후 baseline snapshot

```bash
go run ./hololive/hololive-youtube-producer/cmd/ops/youtube-community-shorts target-baseline
```

2. 같은 시점의 observation key 기록

- `observation_runtime_name`: 예시 `youtube-producer`
- `observation_bigbang_cutover_at`: UTC RFC3339 한 값만 사용

3. 같은 observation key로 게시물별 exact-once / sent-history 수집

```bash
go run ./hololive/hololive-youtube-producer/cmd/ops/youtube-community-shorts send-counts \
  -observation-runtime youtube-producer \
  -observation-cutover <CUTOVER_AT>

go run ./hololive/hololive-youtube-producer/cmd/ops/youtube-community-shorts alarm-sent-history-dataset \
  -observation-runtime youtube-producer \
  -observation-cutover <CUTOVER_AT>

# optional focused drill-down
go run ./hololive/hololive-youtube-producer/cmd/ops/youtube-community-shorts community-alarm-sent-history \
  -observation-runtime youtube-producer \
  -observation-cutover <CUTOVER_AT>

go run ./hololive/hololive-youtube-producer/cmd/ops/youtube-community-shorts shorts-alarm-sent-history \
  -observation-runtime youtube-producer \
  -observation-cutover <CUTOVER_AT>
```

최소 비교 컬럼:

| Dataset | Required columns |
| --- | --- |
| target baseline | `channel_id`, `community_subscribers_key`, `shorts_subscribers_key`, `routes[].alarm_type`, `routes[].alarm_enabled` |
| send-counts | `alarm_type`, `channel_id`, `post_id`, `actual_published_at`, `detected_at`, `alarm_sent_at`, `success_send_count`, `success_room_count`, `duplicate_success_count` |
| sent-history dataset | `results.missing_alarm_evaluated`, `results.missing_alarm_zero`, `results.alarm_type_comparisons[].alarm_type`, `results.alarm_type_comparisons[].missing_alarm_post_count`, `results.channel_comparisons[].channel_id`, `results.channel_comparisons[].missing_alarm_post_count`, `rows[].alarm_type`, `rows[].channel_id`, `rows[].post_key`, `rows[].post_id`, `rows[].content_id`, `rows[].actual_published_at`, `rows[].detected_at`, `rows[].alarm_sent_at`, `verification_rows[].verdict`, `verification_rows[].baseline_count`, `verification_rows[].sent_count`, `reference_rows[].channel_id`, `reference_rows[].channel_post_key`, `reference_rows[].post_id`, `reference_rows[].verification_verdict`, `reference_rows[].sent_count`, `summary.missing_alarm_post_count`, `missing_alarm_rows[].missing_reason`, `missing_alarm_rows[].post_key`, `missing_alarm_rows[].send_state` |
| frozen baseline | `runtime_name`, `bigbang_cutover_at`, `kind`, `post_id`, `channel_id`, `actual_published_at`, `detected_at` |

## 5. Decision Rules For AC1

- 운영 채널 목록은 `target-baseline` 의 `channels[]` 로 확정합니다.
- 실제 알람 기대 채널은 `channels[].routes[]` 중 `alarm_enabled = true` 인 조합으로 확정합니다.
- 공통 기준 시각대는 `UTC canonical` 입니다.
- 게시물·알람 대조 기본 키는 `alarm_type + channel_id + post_id` 입니다.
- raw storage 조인이 필요하면 `kind + canonical_content_id` 를 사용합니다.
- 관찰 종료 후 확정 기준 집합은 `youtube_community_shorts_observation_post_baselines` 의 `(runtime_name, bigbang_cutover_at, kind, post_id)` 를 사용합니다.
- 운영 검증용 정규화 기준 목록은 같은 observation key의 sent-history dataset `reference_rows` 로 읽고, canonical key는 `channel_id + post_id` 입니다.

## Source Of Truth

- 운영 채널 계산: `hololive/hololive-youtube-producer/internal/communityshorts/target_baseline.go`
- baseline 수집: `hololive/hololive-youtube-producer/internal/communityshorts/target_baseline.go`
- repo-known official roster data: `hololive/hololive-shared/pkg/domain/internal/model/data/official_talents.json`
- repo-known official channel profile data: `hololive/hololive-shared/pkg/domain/internal/model/data/official_profiles_raw.json`
- manual non-Hololive member roster seed: `hololive/hololive-kakao-bot-go/scripts/migrations/016-add-multi-group-support.sql`, `017-add-stellive-chzzk-support.sql`, `018-add-twitch-user-id-and-vspo-members.sql`, `040_unify_indie_org.sql`
- typed subscriber key: `hololive/hololive-shared/pkg/service/alarm/keys/keys.go`
- canonical post ID 정규화: `hololive/hololive-shared/pkg/service/youtube/contentid/canonical.go`
- send-count / observation 조인: `hololive/hololive-shared/pkg/service/youtube/outbox/internal/delivery/delivery_post_send_counts.go`
- sent-history dataset collection: `hololive/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/community_shorts_alarm_sent_history_dataset.go`
- observation frozen baseline schema: `hololive/hololive-kakao-bot-go/scripts/migrations/054_create_youtube_community_shorts_observation_post_baselines.sql`
- single-send state schema: `hololive/hololive-kakao-bot-go/scripts/migrations/055_create_youtube_community_shorts_alarm_states.sql`
- channel summary time-basis note: `docs/current/runbooks/YOUTUBE_COMMUNITY_SHORTS_CHANNEL_SUMMARY_LAST_24H.md`
- observation verification key mapping: `docs/current/runbooks/YOUTUBE_COMMUNITY_SHORTS_POST_DEPLOY_24H_VERIFICATION.md`
