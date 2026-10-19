-- +goose Up
-- Repeat a post: once it is published, staqly schedules a copy repeat_every later ('' = no repeat;
-- 1d to 6d, 1w, 2w, 1mo). repeat_of links a copy to the post it repeats, so the copy is made once.
ALTER TABLE posts ADD COLUMN repeat_every TEXT NOT NULL DEFAULT '';
ALTER TABLE posts ADD COLUMN repeat_of TEXT NOT NULL DEFAULT '';
CREATE INDEX posts_repeat_of ON posts (repeat_of) WHERE repeat_of <> '';

-- +goose Down
DROP INDEX posts_repeat_of;
ALTER TABLE posts DROP COLUMN repeat_of;
ALTER TABLE posts DROP COLUMN repeat_every;
