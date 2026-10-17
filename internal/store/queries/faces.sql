-- name: CreateFacePerson :one
INSERT INTO face_persons (tenant_id, display_name, frigate_name, notes, created_by, updated_by)
VALUES (@tenant_id, @display_name, @frigate_name, @notes, @created_by, @created_by)
RETURNING *;

-- name: GetFacePerson :one
SELECT * FROM face_persons WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: ListFacePersons :many
SELECT p.*, (SELECT count(*) FROM face_person_images i WHERE i.person_id = p.id)::int AS image_count
FROM face_persons p
WHERE p.tenant_id = @tenant_id AND p.deleted_at IS NULL
ORDER BY lower(p.display_name), p.id;

-- name: FaceFrigateNameTaken :one
SELECT EXISTS (SELECT 1 FROM face_persons WHERE tenant_id = @tenant_id AND frigate_name = @frigate_name);

-- name: UpdateFacePerson :one
UPDATE face_persons
SET display_name = @display_name, notes = @notes, updated_by = @updated_by, updated_at = now()
WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteFacePerson :one
UPDATE face_persons
SET deleted_at = now(), updated_by = @updated_by, updated_at = now()
WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL
RETURNING *;

-- name: InsertFacePersonImage :one
INSERT INTO face_person_images (id, tenant_id, person_id, storage_key, content_type, size_bytes, sha256, width, height, created_by)
VALUES (@id, @tenant_id, @person_id, @storage_key, @content_type, @size_bytes, @sha256, @width, @height, @created_by)
RETURNING *;

-- name: ListFacePersonImages :many
SELECT * FROM face_person_images WHERE tenant_id = @tenant_id AND person_id = @person_id ORDER BY created_at, id;

-- name: GetFacePersonImage :one
SELECT * FROM face_person_images WHERE tenant_id = @tenant_id AND person_id = @person_id AND id = @id;

-- name: CountFacePersonImages :one
SELECT count(*)::int FROM face_person_images WHERE tenant_id = @tenant_id AND person_id = @person_id;

-- name: DeleteFacePersonImage :exec
DELETE FROM face_person_images WHERE tenant_id = @tenant_id AND person_id = @person_id AND id = @id;

-- name: DeleteFacePersonImages :exec
DELETE FROM face_person_images WHERE tenant_id = @tenant_id AND person_id = @person_id;
