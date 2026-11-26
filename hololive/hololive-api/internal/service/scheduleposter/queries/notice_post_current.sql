SELECT EXISTS (
    SELECT
    FROM schedule_poster_notices n
    WHERE n.id = $1 AND /* fragment:current_change */

)
