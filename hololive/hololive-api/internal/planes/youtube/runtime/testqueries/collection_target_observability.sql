INSERT INTO youtube_collection_projection_generations(status,row_count,projection_sha256,valid_until,activated_at)
VALUES('CURRENT',8,repeat('a',64),now()+interval '1 hour',now());
INSERT INTO youtube_collection_targets(projection_generation,subject_key,observation_kind,priority,poll_interval_ms,enabled,valid_until,created_at)
SELECT generation, subject, kind,20,120000,enabled,now()+duration,now()-interval '10 minutes'
FROM youtube_collection_projection_generations CROSS JOIN (VALUES
 ('stale','viewer_sample',true,interval '1 hour'),
 ('fresh','viewer_sample',true,interval '1 hour'),
 ('missing','viewer_sample',true,interval '1 hour'),
 ('deferred','viewer_sample',true,interval '1 hour'),
 ('disabled','viewer_sample',false,interval '1 hour'),
 ('expired','viewer_sample',true,interval '-1 second'),
 ('content','video_list',true,interval '1 hour'),
 ('content','shorts_list',true,interval '1 hour')
) AS seed(subject,kind,enabled,duration);
INSERT INTO youtube_live_reconciliation_heads(video_id,status)
VALUES('stale','LIVE'),('fresh','LIVE'),('missing','UPCOMING'),('deferred','UPCOMING');
INSERT INTO youtube_live_sessions(video_id,channel_id,status,scheduled_start_time)
VALUES('stale','channel','ENDED',now()-interval '8 days'),
      ('fresh','channel','LIVE',now()-interval '1 hour'),
      ('missing','channel','UPCOMING',now()-interval '8 days'),
      ('deferred','channel','UPCOMING',now()+interval '1 hour');
INSERT INTO youtube_collection_job_leases(job_key,provider,job_class,collection_job_kind,subject_key,
projection_generation,poll_interval_ms,slot_state,scheduled_for,next_due_at,last_completed_at,owner_instance,lease_expires_at,retry_not_before)
SELECT 'collector:youtubejs:youtubejs_viewer:'||subject,'youtubejs','SUBJECT','youtubejs_viewer',subject,
generation,120000,state,now()-interval '10 minutes',now()-interval '5 minutes',now()-age,owner,
CASE WHEN state='ACTIVE' THEN now()+interval '1 minute' END,
CASE WHEN state='DEFERRED' THEN now()+interval '1 minute' END
FROM youtube_collection_projection_generations CROSS JOIN (VALUES
 ('stale','IDLE',interval '10 minutes',null),
 ('fresh','ACTIVE',interval '1 minute','test-owner'),
 ('deferred','DEFERRED',interval '10 minutes',null)
) AS seed(subject,state,age,owner);
