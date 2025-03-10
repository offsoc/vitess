/**
 * Copyright 2021 The Vitess Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
import dayjs from 'dayjs';
import localizedFormat from 'dayjs/plugin/localizedFormat';
import relativeTime from 'dayjs/plugin/relativeTime';

dayjs.extend(localizedFormat);
dayjs.extend(relativeTime);

export const parse = (timestamp: number | Long | null | undefined): dayjs.Dayjs | null => {
    if (typeof timestamp !== 'number') {
        return null;
    }

    // Convert the timestamp to a string and check the number of digits
    let timestampStr = timestamp.toString();

    // If the length of the timestamp is more than 10 digits (seconds resolution),
    // keep dividing by 10 until it has 10 digits
    while (timestampStr.length > 10) {
        timestampStr = Math.floor(timestamp / 10).toString();
        timestamp = timestamp / 10;
    }

    // Now, we assume the timestamp is in seconds resolution and use dayjs.unix()
    return dayjs.unix(timestamp);
};

export const format = (timestamp: number | Long | null | undefined, template: string | undefined): string | null => {
    const u = parse(timestamp);
    return u ? u.format(template) : null;
};

export const formatDateTime = (timestamp: number | Long | null | undefined): string | null => {
    return format(timestamp, 'YYYY-MM-DD LT Z');
};

export const formatRelativeTime = (timestamp: number | Long | null | undefined): string | null => {
    const u = parse(timestamp);
    return u ? u.fromNow() : null;
};

export const formatDateTimeShort = (timestamp: number | Long | null | undefined): string | null => {
    return format(timestamp, 'MM/DD/YY HH:mm:ss Z');
};

export const formatRelativeTimeInSeconds = (timestamp: number | Long | null | undefined): string | null => {
    const u = parse(timestamp);
    if (!u) return null;
    const currentTime = dayjs();
    const secondsElapsed = currentTime.diff(u, 'second');
    return `${secondsElapsed} seconds ago`;
};
