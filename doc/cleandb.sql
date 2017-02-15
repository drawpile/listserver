DELETE FROM sessions WHERE last_active < current_timestamp - interval '7 days';
VACUUM sessions;

