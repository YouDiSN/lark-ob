# Domain context

This glossary keeps the message, memory, knowledge, and recommendation modules aligned. It describes domain meaning rather than storage or API details.

## Person

A stable human identity. Statements made by the same person in direct messages and group conversations belong to the same person, while each statement retains its original conversation as provenance. A display name alone is not a stable identity.

## Conversation

A direct-message or group venue in which people communicate. A conversation provides context and access boundaries; it does not own the identity of the people speaking in it.

## Message evidence

An immutable message received from Lark and retained as provenance. Message evidence may support a memory claim, but raw messages and memory claims are not the same thing.

## Knowledge source

A user-approved external document that may be retrieved when preparing an answer. Each source has an explicit scope and traceable origin. Being readable by the user does not automatically mean it should be imported.

## Knowledge chunk

A retrievable excerpt of a knowledge source. It retains its source document, heading, and block provenance so a recommendation can cite the original material.

## Memory claim

A derived statement about a person, conversation, project, preference, decision, or commitment. It records who or what the claim is about, the evidence that supports it, and its confidence. A person's group-chat statements still attach to that person and also retain the group context.

## Interaction impression

A revisable, user-relative summary of observed communication patterns for one person across conversations. It provides a few display tags and practical communication guidance, but is not a personality diagnosis or a factual memory claim. It is stored as a versioned snapshot with evidence coverage.

## Self voice

The stable baseline of how the local user writes, learned only from messages actually sent by that user. Agent suggestions, bot messages, system messages, forwarded templates, logs, and quoted content must not train this profile.

## Relationship voice

A modifier describing how the local user usually communicates with one stable person identity across conversations. It augments the self voice and must not imitate the other person's style.

## Conversation norm

A local communication convention for one conversation or reusable conversation archetype. It may override a relationship voice when the current venue requires a more formal, concise, or operational style.

## Profile maintenance run

A scheduled derivation pass. The initial run follows the bounded history import; subsequent runs occur once per configured local day, update only source entities with new messages, preserve the previous snapshot on failure, and advance their checkpoint only after success.

## Reply recommendation

A draft response produced from the current conversation, relevant memory claims, and retrieved knowledge. It is always presented for review and is never equivalent to an authorized send action.

## Agent

The Eino-orchestrated, read-only reasoning component. It may call tools that read recent messages, memory claims and scoped knowledge chunks. It has no message-send tool and does not own business data; SQLite repositories remain the source of truth.
