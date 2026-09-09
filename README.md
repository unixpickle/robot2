# robot2

This is an attempt to rewrite a lot of my home robotics stack in a cleaner (human-driven) way.

# Server API

The server provides an HTTP API for controlling the robot and accessing sensor information.
Responses are encapsulated in a JSON payload that either looks like

```json
{ "data": ... }
```

or

```json
{ "error": "some error message" }
```

When an error is returned, the HTTP status code is never 200, and may reflect whether the error
is internal to the robot or has to do with invalid POST request data.

The low-level motors API has some endpoints:

- `/motors/names` - call with a `GET` request; returns JSON array of strings
- `/motors/status` - returns a dictionary mapping motor names to status dictionaries.
- `/motors/limits` - returns a map from motor name to dictionaries with `min` and `max` keys in range `[0, 4095]`.
- `/motors/move` - pass a dictionary with `motor` as the motor name and `pos` as a position in `[0, 4095]`.
- `/motors/stoprelax` - this request blocks indefinitely, and during a call, it prevents a gradual relaxation mechanism that settles motors towards the direction of torque every few seconds. Use this to prevent the robot from "falling" while processing or thinking about state.

A higher level "kinematics" API can control motors with angles:

- `/kinematics/limitsangles` - call with a `GET` request; returns a map with a `min` and `max` key. Each maps to a dictionary of motor names to their corresponding limit angles (in radians).
- `/kinematics/currentangles` - call with a `GET` request to get a map of motor names to current angles.
- `/kinematics/move` - call with a `POST` of a dictionary mapping every motor to its target angle (they must all be specified).

For accessing the cameras:

- `/camera/tracknames` - call with `GET` to get an array of camera track names as strings.
- `/camera/snapshot` - call with a `POST` with a dictionary containing a `track` key mapping to the track name. When successful, status 200 with JPEG bytes body is returned; otherwise a non-200 status code is returned with a JSON object containing an `error` key.
