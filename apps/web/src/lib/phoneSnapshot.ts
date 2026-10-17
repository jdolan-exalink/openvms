/** Where the gateway serves the latest frame; it sits under players and is the whole picture of grid tiles. */
export const snapshotUrl = (id: string) => `/media/v1/cameras/${id}/snapshot.jpg?h=360`;

export const statusDot = (status?: string) => (status === "online" ? "bg-ok" : status === "offline" ? "bg-bad" : "bg-warn");
