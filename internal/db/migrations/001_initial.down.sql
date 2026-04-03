DROP TRIGGER IF EXISTS tracks_updated_at  ON tracks;
DROP TRIGGER IF EXISTS albums_updated_at  ON albums;
DROP TRIGGER IF EXISTS artists_updated_at ON artists;

DROP FUNCTION IF EXISTS update_updated_at;

DROP TABLE IF EXISTS tracks;
DROP TABLE IF EXISTS artist_genres;
DROP TABLE IF EXISTS albums;
DROP TABLE IF EXISTS artists;
DROP TABLE IF EXISTS genres;