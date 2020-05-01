# watermeter

Original wiring for meter pluse comes from https://github.com/Freenove/Freenove_Ultimate_Starter_Kit_for_Raspberry_Pi/blob/master/Tutorial.pdf, chapter 2 (the button wiring).


# DB creation (poor man's migrations)

create table meter (
   id serial primary key,
   recorded_at timestamp not null
);
