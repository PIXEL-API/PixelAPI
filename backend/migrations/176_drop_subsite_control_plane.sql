-- Subsite control-plane cleanup is intentionally deferred.
-- The current application code no longer references these tables or settings,
-- but dropping production tables should be handled in a separate maintenance
-- migration after explicit operational approval.
SELECT 1;
