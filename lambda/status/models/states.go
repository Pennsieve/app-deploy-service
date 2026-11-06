package models

const StateStopped = "STOPPED"

// StopCodeTaskFailedToStart is the stop code of a task that never ran: its
// image couldn't be pulled (a registry outage), no capacity, ... Its
// containers have no exit code, so only the stop code says it failed.
const StopCodeTaskFailedToStart = "TaskFailedToStart"
