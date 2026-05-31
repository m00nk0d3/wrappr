-- name: CreateInvitation :one
INSERT INTO invitations (company_id, email, role, token, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetInvitationByToken :one
SELECT * FROM invitations WHERE token = $1;

-- name: GetValidInvitationByToken :one
SELECT * FROM invitations
WHERE token = $1
  AND accepted_at IS NULL
  AND expires_at > NOW();

-- name: GetPendingInvitationByEmailAndCompany :one
SELECT * FROM invitations
WHERE company_id = $1
  AND email = $2
  AND accepted_at IS NULL
  AND expires_at > NOW();

-- name: AcceptInvitation :one
UPDATE invitations
SET accepted_at = NOW()
WHERE token = $1
  AND accepted_at IS NULL
  AND expires_at > NOW()
RETURNING *;
