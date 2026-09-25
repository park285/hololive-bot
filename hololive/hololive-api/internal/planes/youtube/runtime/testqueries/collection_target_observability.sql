INSERT INTO youtube_collection_projection_generations(status,row_count,projection_sha256,valid_until,activated_at)
VALUES('CURRENT',12,repeat('a',64),now()+interval '1 hour',now()),
      ('RETIRED',2,repeat('b',64),now()+interval '1 hour',now()-interval '1 hour');
INSERT INTO youtube_collection_targets(projection_generation,subject_key,observation_kind,priority,poll_interval_ms,enabled,valid_until,created_at)
SELECT generation, subject, kind,20,120000,enabled,now()+duration,now()-interval '10 minutes'
FROM youtube_collection_projection_generations CROSS JOIN (VALUES
 ('stale','live_snapshot',true,interval '1 hour'),
 ('fresh','live_snapshot',true,interval '1 hour'),
 ('missing','live_snapshot',true,interval '1 hour'),
 ('deferred','live_snapshot',true,interval '1 hour'),
 ('disabled','live_snapshot',false,interval '1 hour'),
 ('expired','live_snapshot',true,interval '-1 second'),
 ('content','video_list',true,interval '1 hour'),
 ('content','shorts_list',true,interval '1 hour'),
 ('notify','community_page',true,interval '1 hour'),
 ('metadata','channel_stats',true,interval '1 hour'),
 ('metadata','channel_profile',true,interval '1 hour'),
 ('metadata','channel_photo',true,interval '1 hour')
) AS seed(subject,kind,enabled,duration)
WHERE status='CURRENT';
INSERT INTO youtube_collection_targets(projection_generation,subject_key,observation_kind,priority,poll_interval_ms,enabled,valid_until)
SELECT generation,subject,kind,20,120000,true,now()+interval '1 hour'
FROM youtube_collection_projection_generations CROSS JOIN (VALUES
 ('historical-viewer','viewer_sample'),('retired','live_snapshot')
) AS seed(subject,kind)
WHERE status='RETIRED';
INSERT INTO youtube_live_reconciliation_heads(video_id,status)
VALUES('video-stale','LIVE'),('video-fresh','LIVE'),
      ('video-missing','UPCOMING'),('video-deferred','UPCOMING'),('recent-overdue','UPCOMING'),
      ('ended-head','ENDED'),('fully-ended','ENDED'),
      ('video-disabled','LIVE'),('video-expired','LIVE'),('video-outside','LIVE'),
      ('video-retired','LIVE'),('head-without-session','LIVE');
INSERT INTO youtube_live_sessions(video_id,channel_id,status,scheduled_start_time)
VALUES('video-stale','stale','ENDED',now()-interval '8 days'),
      ('video-fresh','fresh','LIVE',now()-interval '1 hour'),
      ('video-missing','missing','UPCOMING',now()-interval '8 days'),
      ('video-deferred','deferred','UPCOMING',now()+interval '1 hour'),
      ('recent-overdue','stale','UPCOMING',now()-interval '1 hour'),
      ('no-head','fresh','LIVE',now()-interval '1 hour'),
      ('ended-head','fresh','UPCOMING',now()-interval '1 hour'),
      ('fully-ended','fresh','ENDED',now()-interval '8 days'),
      ('video-disabled','disabled','LIVE',now()-interval '1 hour'),
      ('video-expired','expired','LIVE',now()-interval '1 hour'),
      ('video-outside','outside','LIVE',now()-interval '1 hour'),
      ('video-retired','retired','LIVE',now()-interval '1 hour');
INSERT INTO youtube_collection_job_leases(job_key,provider,job_class,collection_job_kind,subject_key,
projection_generation,poll_interval_ms,slot_state,scheduled_for,next_due_at,last_completed_at,owner_instance,lease_expires_at,retry_not_before)
SELECT 'collector:youtubejs:youtubejs_channel_live:'||subject,'youtubejs','SUBJECT','youtubejs_channel_live',subject,
generation,120000,state,now()-interval '10 minutes',now()-interval '5 minutes',now()-age,owner,
CASE WHEN state='ACTIVE' THEN now()+interval '1 minute' END,
CASE WHEN state='DEFERRED' THEN now()+interval '1 minute' END
FROM youtube_collection_projection_generations CROSS JOIN (VALUES
 ('stale','IDLE',interval '10 minutes',null),
 ('fresh','ACTIVE',interval '1 minute','test-owner'),
 ('deferred','DEFERRED',interval '10 minutes',null)
) AS seed(subject,state,age,owner)
WHERE status='CURRENT';
